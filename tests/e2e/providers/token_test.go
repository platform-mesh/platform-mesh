/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const tokenExpiresAtAnnotation = "providers.platform-mesh.io/token-expires-at"

// TestProviderShortLivedToken checks that the Provider kubeconfig carries a token
// that expires, that it works, and that the Provider controller renews it.
func TestProviderShortLivedToken(t *testing.T) {
	ctx := t.Context()
	cl := newProviderWorkspace(t)
	provider := createReadyProvider(t, cl, &pmprovidersv1alpha1.Provider{ObjectMeta: metav1.ObjectMeta{Name: "e2e-token"}})

	secret := kubeconfigSecret(t, cl, provider)
	token := string(secret.Data["token"])
	require.NotEmpty(t, token, "the Secret must carry the token next to the kubeconfig")
	expiresAt, err := time.Parse(time.RFC3339, secret.Annotations[tokenExpiresAtAnnotation])
	require.NoError(t, err, "annotation %s", tokenExpiresAtAnnotation)

	t.Run("the token expires", func(t *testing.T) {
		claims := jwtClaims(t, token)
		exp, iat := claims["exp"].(float64), claims["iat"].(float64)
		require.NotZero(t, exp, "a token without exp never expires")
		lifetime := time.Duration(exp-iat) * time.Second
		assert.LessOrEqual(t, lifetime, 24*time.Hour)
		assert.WithinDuration(t, time.Unix(int64(exp), 0), expiresAt, time.Minute, "the annotation must match the token")
	})

	t.Run("the kubeconfig reads the token file next to it", func(t *testing.T) {
		kubeconfig, err := clientcmd.Load(secret.Data["kubeconfig"])
		require.NoError(t, err)
		auth := kubeconfig.AuthInfos[kubeconfig.Contexts[kubeconfig.CurrentContext].AuthInfo]
		assert.Equal(t, token, auth.Token)
		assert.Equal(t, "token", auth.TokenFile)
	})

	cluster := providerCluster(t, secret.Data["kubeconfig"])

	t.Run("the kubeconfig gives access to the provider workspace", func(t *testing.T) {
		providerCl, err := pm.NewClient(pm.Kubeconfig(t, secret.Data["kubeconfig"], cluster))
		require.NoError(t, err)
		require.NoError(t, providerCl.List(ctx, &corev1.NamespaceList{}))
	})

	t.Run("no token Secret that never expires", func(t *testing.T) {
		secrets := &corev1.SecretList{}
		require.NoError(t, kcpClient(t, cluster).List(ctx, secrets, ctrlruntimeclient.InNamespace("default")))
		for _, s := range secrets.Items {
			assert.NotEqual(t, corev1.SecretTypeServiceAccountToken, s.Type, "Secret %s holds a token without expiry", s.Name)
		}
	})

	t.Run("the controller renews the token before it expires", func(t *testing.T) {
		// Pretend the token is about to expire, then make the controller look at it.
		patch := ctrlruntimeclient.MergeFrom(secret.DeepCopy())
		secret.Annotations[tokenExpiresAtAnnotation] = time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
		require.NoError(t, cl.Patch(ctx, secret, patch))
		touch := ctrlruntimeclient.MergeFrom(provider.DeepCopy())
		if provider.Annotations == nil {
			provider.Annotations = map[string]string{}
		}
		provider.Annotations["e2e.platform-mesh.io/touch"] = time.Now().Format(time.RFC3339Nano)
		require.NoError(t, cl.Patch(ctx, provider, touch))

		wait.Eventually(t, func() (bool, string) {
			renewed := &corev1.Secret{}
			if err := cl.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(secret), renewed); err != nil {
				return false, err.Error()
			}
			if string(renewed.Data["token"]) == token {
				return false, "token not renewed yet"
			}
			return true, ""
		}, providerTimeout, pollInterval, "token renewal")
	})

	t.Run("deleting the Provider deletes the provider workspace", func(t *testing.T) {
		require.NoError(t, cl.Delete(context.Background(), provider))
		wait.Eventually(t, func() (bool, string) {
			err := kcpClient(t, cluster).List(ctx, &corev1.NamespaceList{})
			if err == nil {
				return false, "provider workspace still serves requests"
			}
			return true, ""
		}, providerTimeout, pollInterval, "deletion of logical cluster %s", cluster)
	})
}

// jwtClaims decodes the payload of a JWT without checking its signature.
func jwtClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3, "not a JWT")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	claims := map[string]any{}
	require.NoError(t, json.Unmarshal(payload, &claims))
	return claims
}
