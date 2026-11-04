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

package access

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	accessv1alpha1 "go.platform-mesh.io/sdk/access/apis/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// testAccessRevokesExpiredToken: when the kubeconfig's SA token has an
// exp in the past, the Access reconciler marks the Access Revoked
// without attempting a TokenRequest. This exercises the renewal
// reconciler's terminal branch without needing a real SA in kcp.
func testAccessRevokesExpiredToken(t *testing.T, e *env) {
	ctx := t.Context()
	name := "test-access-revoked"

	kubeconfigSecretName := name + "-kubeconfig"
	expiredToken := saJWT(t, "default", "sa-does-not-exist", time.Now().Add(-1*time.Hour))
	kubeconfig := kubeconfigYAML(t, expiredToken)

	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: kubeconfigSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{"kubeconfig": kubeconfig},
	}
	require.NoError(t, e.cl.Create(ctx, sec))

	// Access objects reference the identity by name; the reconciler
	// does not read it, but the CRD schema still requires the field.
	acc := &accessv1alpha1.Access{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: accessv1alpha1.AccessSpec{
			IdentityRef:         corev1.LocalObjectReference{Name: "unused-identity"},
			KubeconfigSecretRef: &corev1.LocalObjectReference{Name: kubeconfigSecretName},
		},
	}
	require.NoError(t, e.cl.Create(ctx, acc))

	wait.Until(t, func() bool {
		cur := &accessv1alpha1.Access{}
		if err := e.cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: testNamespace}, cur); err != nil {
			return false
		}
		return cur.Status.Phase == accessv1alpha1.AccessPhaseRevoked
	}, defaultTestTimeout, defaultTickInterval, "Access with expired SA token should go Revoked")
}

// saJWT returns an unsigned JWT whose "sub" is the ServiceAccount
// namespace/name and "exp" is the given time, mirroring what kcp's
// TokenRequest produces. The reconciler only decodes the claims (no
// signature check) so an unsigned token is enough.
func saJWT(t *testing.T, namespace, name string, exp time.Time) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	require.NoError(t, err)
	claims, err := json.Marshal(map[string]any{
		"sub": "system:serviceaccount:" + namespace + ":" + name,
		"exp": exp.Unix(),
	})
	require.NoError(t, err)
	enc := base64.RawURLEncoding.EncodeToString
	return enc(header) + "." + enc(claims) + "."
}

// testAccessRenewsExpiringToken: with a real SA in a kcp workspace and
// RBAC to self-TokenRequest, the Access reconciler mints a fresh SA
// token and writes it into the kubeconfig Secret before the current
// one expires. The initial token is bootstrapped by the test using
// admin creds (minimum TTL 10 min, below the default 12 min renewal
// threshold, so the first reconcile renews).
func testAccessRenewsExpiringToken(t *testing.T, e *env) {
	ctx := t.Context()
	const saName = "renewer"
	const accName = "test-access-renew"
	kubeconfigSecretName := accName + "-kubeconfig"

	// SA + Role + RoleBinding granting self-TokenRequest.
	require.NoError(t, e.cl.Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: testNamespace},
	}))
	require.NoError(t, e.cl.Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "renew-self", Namespace: testNamespace},
		Rules: []rbacv1.PolicyRule{{
			APIGroups:     []string{""},
			Resources:     []string{"serviceaccounts/token"},
			ResourceNames: []string{saName},
			Verbs:         []string{"create"},
		}},
	}))
	require.NoError(t, e.cl.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "renew-self", Namespace: testNamespace},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: saName, Namespace: testNamespace}},
		RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "renew-self", APIGroup: "rbac.authorization.k8s.io"},
	}))

	// Bootstrap the first token via admin creds. 600s is the vanilla
	// minimum; kcp honors the same floor.
	adminCS, err := kubernetes.NewForConfig(e.adminCfg)
	require.NoError(t, err)
	ttl := int64(600)
	tr, err := adminCS.CoreV1().ServiceAccounts(testNamespace).CreateToken(ctx, saName,
		&authv1.TokenRequest{Spec: authv1.TokenRequestSpec{ExpirationSeconds: &ttl}},
		metav1.CreateOptions{})
	require.NoError(t, err)
	firstToken := tr.Status.Token
	require.NotEmpty(t, firstToken)

	// Kubeconfig points at the kcp workspace with that token.
	kubeconfig := kubeconfigForKcp(t, e.adminCfg, firstToken)
	require.NoError(t, e.cl.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: kubeconfigSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{"kubeconfig": kubeconfig},
	}))

	// Access referencing the kubeconfig Secret kicks the reconciler.
	require.NoError(t, e.cl.Create(ctx, &accessv1alpha1.Access{
		ObjectMeta: metav1.ObjectMeta{Name: accName, Namespace: testNamespace},
		Spec: accessv1alpha1.AccessSpec{
			IdentityRef:         corev1.LocalObjectReference{Name: "unused-identity"},
			KubeconfigSecretRef: &corev1.LocalObjectReference{Name: kubeconfigSecretName},
		},
	}))

	// The Secret's kubeconfig token should flip to a fresh one.
	wait.Until(t, func() bool {
		sec := &corev1.Secret{}
		if err := e.cl.Get(ctx, types.NamespacedName{Name: kubeconfigSecretName, Namespace: testNamespace}, sec); err != nil {
			return false
		}
		cfg, err := clientcmd.Load(sec.Data["kubeconfig"])
		if err != nil {
			return false
		}
		for _, ai := range cfg.AuthInfos {
			if ai != nil && ai.Token != "" && ai.Token != firstToken {
				return true
			}
		}
		return false
	}, 60*time.Second, 500*time.Millisecond, "kubeconfig token should be renewed")

	// status.lastRefreshed set by the renewal update.
	cur := &accessv1alpha1.Access{}
	require.NoError(t, e.cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accName, Namespace: testNamespace}, cur))
	require.NotNil(t, cur.Status.LastRefreshed)
}

// kubeconfigForKcp builds a kubeconfig that talks to the kcp workspace
// at cfg.Host with the given bearer token, trusting cfg's CA bundle.
func kubeconfigForKcp(t *testing.T, cfg *rest.Config, token string) []byte {
	t.Helper()
	api := clientcmdapi.NewConfig()
	cluster := &clientcmdapi.Cluster{Server: cfg.Host}
	if len(cfg.CAData) > 0 {
		cluster.CertificateAuthorityData = cfg.CAData
	} else {
		cluster.InsecureSkipTLSVerify = true
	}
	api.Clusters["kcp"] = cluster
	api.AuthInfos["sa"] = &clientcmdapi.AuthInfo{Token: token}
	api.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "kcp", AuthInfo: "sa"}
	api.CurrentContext = "ctx"
	out, err := clientcmd.Write(*api)
	require.NoError(t, err)
	return out
}

// kubeconfigYAML returns a minimal kubeconfig pointing at a dummy
// cluster, with token in the sole AuthInfo. The reconciler only reads
// the current-context's token to decide whether to renew.
func kubeconfigYAML(t *testing.T, token string) []byte {
	t.Helper()
	// Dummy server is fine: the reconciler's Revoked branch returns
	// before calling TokenRequest.
	raw := []byte(`apiVersion: v1
kind: Config
clusters:
- name: cluster
  cluster:
    server: https://127.0.0.1:6443
    insecure-skip-tls-verify: true
contexts:
- name: ctx
  context:
    cluster: cluster
    user: user
current-context: ctx
users:
- name: user
  user:
    token: ` + token + `
`)
	return raw
}
