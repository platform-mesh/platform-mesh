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
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	oidcAudience  = "kcp"
	oidcTokenFile = "/var/run/secrets/kcp/token"
	// vwAuthEnv turns on the check through the APIExport virtual workspace; it needs a
	// kcp that applies workspace authentication to virtual workspace URLs.
	vwAuthEnv = "E2E_KCP_VW_WORKSPACE_AUTH"
)

// TestProviderOIDC creates a Provider whose controllers log in with ServiceAccount
// tokens of the kind cluster: kcp trusts that issuer for the provider workspace only.
func TestProviderOIDC(t *testing.T) {
	ctx := t.Context()
	kube := kubernetes.NewForConfigOrDie(pm.Kube(t))
	namespace, serviceAccount := oidcIdentity(t, kube)
	issuer, clusterCA := oidcIssuer(t, kube)
	subject := fmt.Sprintf("system:serviceaccount:%s:%s", namespace, serviceAccount)

	cl := newProviderWorkspace(t)
	provider := createReadyProvider(t, cl, &pmprovidersv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-oidc"},
		Spec: pmprovidersv1alpha1.ProviderSpec{Authentication: &pmprovidersv1alpha1.ProviderAuthentication{OIDC: &pmprovidersv1alpha1.ProviderOIDCAuthentication{
			IssuerURL:            issuer,
			DiscoveryURL:         "https://kubernetes.default.svc/.well-known/openid-configuration",
			CertificateAuthority: clusterCA,
			Audiences:            []string{oidcAudience},
			Subjects:             []string{subject},
		}}},
	})
	wsName := provider.Name + "-" + provider.Annotations["kcp.io/cluster"]
	root := kcpClient(t, "root")

	t.Run("the provider workspace has its own type with the issuer", func(t *testing.T) {
		authConfig := &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{}
		require.NoError(t, root.Get(ctx, ctrlruntimeclient.ObjectKey{Name: wsName}, authConfig))
		require.Len(t, authConfig.Spec.JWT, 1)
		assert.Equal(t, issuer, authConfig.Spec.JWT[0].Issuer.URL)
		wsType := &kcptenancyv1alpha1.WorkspaceType{}
		require.NoError(t, root.Get(ctx, ctrlruntimeclient.ObjectKey{Name: wsName}, wsType))
		assert.Equal(t, []kcptenancyv1alpha1.AuthenticationConfigurationReference{{Name: wsName}}, wsType.Spec.AuthenticationConfigurations)
		ws := &kcptenancyv1alpha1.Workspace{}
		require.NoError(t, kcpClient(t, "root:providers").Get(ctx, ctrlruntimeclient.ObjectKey{Name: wsName}, ws))
		assert.Equal(t, kcptenancyv1alpha1.WorkspaceTypeName(wsName), ws.Spec.Type.Name)
	})

	secret := kubeconfigSecret(t, cl, provider)
	cluster := providerCluster(t, secret.Data["kubeconfig"])

	t.Run("the kubeconfig has no token", func(t *testing.T) {
		kubeconfig, err := clientcmd.Load(secret.Data["kubeconfig"])
		require.NoError(t, err)
		auth := kubeconfig.AuthInfos[kubeconfig.Contexts[kubeconfig.CurrentContext].AuthInfo]
		assert.Empty(t, auth.Token)
		assert.Equal(t, oidcTokenFile, auth.TokenFile)
		assert.NotContains(t, secret.Data, "token")
	})

	token := serviceAccountToken(t, kube, namespace, serviceAccount, oidcAudience)
	// The test has its own token; the token file exists only in the pods of the provider.
	parsed, err := clientcmd.Load(secret.Data["kubeconfig"])
	require.NoError(t, err)
	for _, auth := range parsed.AuthInfos {
		auth.TokenFile = ""
	}
	withoutTokenFile, err := clientcmd.Write(*parsed)
	require.NoError(t, err)
	withToken := func(t *testing.T, token, path string) *rest.Config {
		cfg := rest.CopyConfig(pm.Kubeconfig(t, withoutTokenFile, cluster))
		cfg.BearerToken, cfg.BearerTokenFile = token, ""
		if path != cluster {
			cfg.Host = strings.Replace(cfg.Host, "/clusters/"+cluster, "/clusters/"+path, 1)
		}
		return cfg
	}

	t.Run("a ServiceAccount token of the cluster logs in, with the provider prefix", func(t *testing.T) {
		review, err := kubernetes.NewForConfigOrDie(withToken(t, token, cluster)).AuthenticationV1().SelfSubjectReviews().Create(ctx, &authenticationv1.SelfSubjectReview{}, metav1.CreateOptions{})
		require.NoError(t, err)
		assert.Equal(t, wsName+":"+subject, review.Status.UserInfo.Username)
		_, err = kubernetes.NewForConfigOrDie(withToken(t, token, cluster)).CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		require.NoError(t, err, "the subject has full access to the provider workspace")
	})

	t.Run("a token with another audience is rejected", func(t *testing.T) {
		other := serviceAccountToken(t, kube, namespace, serviceAccount, "not-kcp")
		_, err := kubernetes.NewForConfigOrDie(withToken(t, other, cluster)).CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		assert.True(t, apierrors.IsUnauthorized(err), "got %v", err)
	})

	t.Run("the token works in no other workspace", func(t *testing.T) {
		_, err := kubernetes.NewForConfigOrDie(withToken(t, token, "root:providers:system")).CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		assert.True(t, apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err), "got %v", err)
	})

	t.Run("the token works on the APIExport virtual workspace", func(t *testing.T) {
		if os.Getenv(vwAuthEnv) != "true" {
			t.Skipf("set %s=true with a kcp that applies workspace authentication to virtual workspaces", vwAuthEnv)
		}
		providerCl, err := pm.NewClient(withToken(t, token, cluster))
		require.NoError(t, err)
		export := &kcpapisv1alpha2.APIExport{ObjectMeta: metav1.ObjectMeta{Name: "e2e-oidc.example.com"}}
		require.NoError(t, providerCl.Create(ctx, export))
		// kcp lists endpoints only for shards where the export is bound.
		consumer := newConsumerWorkspace(t)
		binding := &kcpapisv1alpha2.APIBinding{
			ObjectMeta: metav1.ObjectMeta{Name: export.Name},
			Spec:       kcpapisv1alpha2.APIBindingSpec{Reference: kcpapisv1alpha2.BindingReference{Export: &kcpapisv1alpha2.ExportBindingReference{Path: cluster, Name: export.Name}}},
		}
		require.NoError(t, consumer.Create(ctx, binding))
		wait.APIBindingBound(t, consumer, binding.Name)
		slice := wait.For(t, providerTimeout, pollInterval, "APIExportEndpointSlice URL", func() (*kcpapisv1alpha1.APIExportEndpointSlice, error) {
			s := &kcpapisv1alpha1.APIExportEndpointSlice{}
			if err := providerCl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: export.Name}, s); err != nil {
				return nil, fmt.Errorf("%w: %v", wait.ErrNotReady, err)
			}
			if len(s.Status.APIExportEndpoints) == 0 {
				return nil, fmt.Errorf("%w: no endpoints yet", wait.ErrNotReady)
			}
			return s, nil
		})
		cfg := withToken(t, token, cluster)
		cfg.Host = slice.Status.APIExportEndpoints[0].URL + "/clusters/*"
		status := 0
		kubernetes.NewForConfigOrDie(cfg).Discovery().RESTClient().Get().AbsPath("/apis").Do(ctx).StatusCode(&status)
		assert.Equal(t, http.StatusOK, status)
	})

	t.Run("deleting the Provider removes its authentication", func(t *testing.T) {
		require.NoError(t, cl.Delete(context.Background(), provider))
		wait.Eventually(t, func() (bool, string) {
			err := root.Get(ctx, ctrlruntimeclient.ObjectKey{Name: wsName}, &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{})
			if !apierrors.IsNotFound(err) {
				return false, fmt.Sprintf("WorkspaceAuthenticationConfiguration %s: %v", wsName, err)
			}
			err = root.Get(ctx, ctrlruntimeclient.ObjectKey{Name: wsName}, &kcptenancyv1alpha1.WorkspaceType{})
			if !apierrors.IsNotFound(err) {
				return false, fmt.Sprintf("WorkspaceType %s: %v", wsName, err)
			}
			return true, ""
		}, providerTimeout, pollInterval, "cleanup of %s", wsName)
	})
}

// newConsumerWorkspace creates an empty workspace to bind an APIExport in.
func newConsumerWorkspace(t *testing.T) ctrlruntimeclient.Client {
	t.Helper()
	name := fmt.Sprintf("e2e-consumer-%06d", rand.IntN(1000000))
	root := kcpClient(t, "root")
	ws := &kcptenancyv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       kcptenancyv1alpha1.WorkspaceSpec{Type: &kcptenancyv1alpha1.WorkspaceTypeReference{Name: "universal", Path: "root"}},
	}
	require.NoError(t, root.Create(t.Context(), ws))
	t.Cleanup(func() {
		_ = root.Delete(context.Background(), ws)
	})
	wait.WorkspaceReadyWithin(t, root, name, 2*time.Minute, pollInterval)
	return kcpClient(t, "root:"+name)
}

// oidcIdentity creates a namespace and ServiceAccount in the kind cluster, and lets
// kcp read the OpenID discovery document of the cluster without login.
func oidcIdentity(t *testing.T, kube kubernetes.Interface) (string, string) {
	t.Helper()
	ctx := t.Context()
	namespace := fmt.Sprintf("e2e-oidc-%06d", rand.IntN(1000000))
	_, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = kube.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
	})
	_, err = kube.CoreV1().ServiceAccounts(namespace).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "controller"}}, metav1.CreateOptions{})
	require.NoError(t, err)

	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-oidc-discovery-unauthenticated"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "system:service-account-issuer-discovery"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.GroupKind, Name: "system:unauthenticated"}},
	}
	if _, err := kube.RbacV1().ClusterRoleBindings().Create(ctx, binding, metav1.CreateOptions{}); err == nil {
		t.Cleanup(func() {
			_ = kube.RbacV1().ClusterRoleBindings().Delete(context.Background(), binding.Name, metav1.DeleteOptions{})
		})
	} else {
		require.True(t, apierrors.IsAlreadyExists(err), "creating %s: %v", binding.Name, err)
	}
	return namespace, "controller"
}

// oidcIssuer returns the ServiceAccount issuer of the kind cluster and its CA.
func oidcIssuer(t *testing.T, kube kubernetes.Interface) (string, string) {
	t.Helper()
	raw, err := kube.Discovery().RESTClient().Get().AbsPath("/.well-known/openid-configuration").DoRaw(t.Context())
	require.NoError(t, err)
	discovery := struct {
		Issuer string `json:"issuer"`
	}{}
	require.NoError(t, json.Unmarshal(raw, &discovery))
	ca, err := kube.CoreV1().ConfigMaps("default").Get(t.Context(), "kube-root-ca.crt", metav1.GetOptions{})
	require.NoError(t, err)
	return discovery.Issuer, ca.Data["ca.crt"]
}

func serviceAccountToken(t *testing.T, kube kubernetes.Interface, namespace, name, audience string) string {
	t.Helper()
	request, err := kube.CoreV1().ServiceAccounts(namespace).CreateToken(t.Context(), name, &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{Audiences: []string{audience}, ExpirationSeconds: ptr.To(int64(600))},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	return request.Status.Token
}
