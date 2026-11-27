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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func newOIDCProvider() *pmprovidersv1alpha1.Provider {
	return &pmprovidersv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{Name: "wildwest", Annotations: map[string]string{"kcp.io/cluster": "abc123"}},
		Spec: pmprovidersv1alpha1.ProviderSpec{
			Authentication: &pmprovidersv1alpha1.ProviderAuthentication{
				OIDC: &pmprovidersv1alpha1.ProviderOIDCAuthentication{
					IssuerURL:    "https://kubernetes.default.svc.cluster.local",
					DiscoveryURL: "https://issuer.example.com/.well-known/openid-configuration",
					Audiences:    []string{"kcp"},
					Subjects:     []string{"system:serviceaccount:wildwest:controller"},
				},
			},
		},
	}
}

func newFakeClient(t *testing.T, objs ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, kcptenancyv1alpha1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func newLoggerCtx(t *testing.T) context.Context {
	t.Helper()
	cfg := logger.DefaultConfig()
	cfg.NoJSON = true
	log, err := logger.New(cfg)
	require.NoError(t, err)
	return context.WithValue(context.Background(), keys.LoggerCtxKey, log)
}

func TestProviderWorkspaceType(t *testing.T) {
	plain := newOIDCProvider()
	plain.Spec.Authentication = nil
	assert.Equal(t, &kcptenancyv1alpha1.WorkspaceTypeReference{Name: "provider", Path: "root"}, providerWorkspaceType(plain))

	assert.Equal(t, &kcptenancyv1alpha1.WorkspaceTypeReference{Name: "wildwest-abc123", Path: "root"}, providerWorkspaceType(newOIDCProvider()))
}

func TestEnsureAndDeleteOIDCWorkspaceType(t *testing.T) {
	ctx := context.Background()
	root := newFakeClient(t)
	provider := newOIDCProvider()

	require.NoError(t, ensureOIDCWorkspaceType(ctx, root, provider))
	require.NoError(t, ensureOIDCWorkspaceType(ctx, root, provider), "ensure must be idempotent")

	authConfig := &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{}
	require.NoError(t, root.Get(ctx, types.NamespacedName{Name: "wildwest-abc123"}, authConfig))
	require.Len(t, authConfig.Spec.JWT, 1)
	jwt := authConfig.Spec.JWT[0]
	assert.Equal(t, "https://kubernetes.default.svc.cluster.local", jwt.Issuer.URL)
	assert.Equal(t, "https://issuer.example.com/.well-known/openid-configuration", jwt.Issuer.DiscoveryURL)
	assert.Equal(t, []string{"kcp"}, jwt.Issuer.Audiences)
	assert.Equal(t, "sub", jwt.ClaimMappings.Username.Claim)
	require.NotNil(t, jwt.ClaimMappings.Username.Prefix)
	assert.Equal(t, "wildwest-abc123:", *jwt.ClaimMappings.Username.Prefix, "the prefix keeps the identities of one provider apart")
	assert.Equal(t, "groups", jwt.ClaimMappings.Groups.Claim, "kcp rejects a JWT authenticator without a groups mapping")

	wsType := &kcptenancyv1alpha1.WorkspaceType{}
	require.NoError(t, root.Get(ctx, types.NamespacedName{Name: "wildwest-abc123"}, wsType))
	assert.Equal(t, []kcptenancyv1alpha1.WorkspaceTypeReference{{Name: "provider", Path: "root"}}, wsType.Spec.Extend.With)
	assert.Equal(t, []kcptenancyv1alpha1.AuthenticationConfigurationReference{{Name: "wildwest-abc123"}}, wsType.Spec.AuthenticationConfigurations)

	require.NoError(t, deleteOIDCWorkspaceType(ctx, root, provider))
	require.NoError(t, deleteOIDCWorkspaceType(ctx, root, provider), "delete must ignore missing objects")
	assert.True(t, apierrors.IsNotFound(root.Get(ctx, types.NamespacedName{Name: "wildwest-abc123"}, wsType)))
}

func TestWorkspaceSubroutineUsesOIDCWorkspaceType(t *testing.T) {
	ctx := newLoggerCtx(t)
	local := new(mocks.Client)
	local.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
			obj.(*corev1.Secret).Data = map[string][]byte{"ca.crt": []byte("ca"), "tls.crt": []byte("crt"), "tls.key": []byte("key")}
			return nil
		})
	root, providers := newFakeClient(t), newFakeClient(t)
	helper := new(mocks.KcpHelper)
	helper.EXPECT().NewKcpClient(mock.Anything, "root").Return(root, nil)
	helper.EXPECT().NewKcpClient(mock.Anything, "root:providers").Return(providers, nil)

	sub, err := NewProviderWorkspaceSubroutine(local, helper, config.KCPConfig{ClusterAdminSecretName: "kcp-admin", Namespace: "platform-mesh-system"}, "https://kcp.api.example.com")
	require.NoError(t, err)

	result, err := sub.Process(ctx, newOIDCProvider())
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue(), "the new workspace is not Ready yet")

	ws := &kcptenancyv1alpha1.Workspace{}
	require.NoError(t, providers.Get(ctx, types.NamespacedName{Name: "wildwest-abc123"}, ws))
	assert.Equal(t, &kcptenancyv1alpha1.WorkspaceTypeReference{Name: "wildwest-abc123", Path: "root"}, ws.Spec.Type)
	require.NoError(t, root.Get(ctx, types.NamespacedName{Name: "wildwest-abc123"}, &kcptenancyv1alpha1.WorkspaceType{}), "the type must exist before the workspace")
}

func TestProcessOIDC(t *testing.T) {
	ctx := newLoggerCtx(t)
	providerWs := newFakeClient(t, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "kube-root-ca.crt", Namespace: "default"},
		Data:       map[string]string{"ca.crt": "fake-ca"},
	})
	userWs := newFakeClient(t)
	sub := NewScopedKubeconfigSubroutine(nil, nil, config.KCPConfig{}, "https://kcp.api.example.com", nil)
	provider := newOIDCProvider()
	ws := &kcptenancyv1alpha1.Workspace{Spec: kcptenancyv1alpha1.WorkspaceSpec{Cluster: "wildwest-cluster"}}

	result, err := sub.processOIDC(ctx, provider, ws, providerWs, userWs)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	assert.Equal(t, pmprovidersv1alpha1.ProviderPhaseReady, provider.Status.Phase)

	binding := &rbacv1.ClusterRoleBinding{}
	require.NoError(t, providerWs.Get(ctx, types.NamespacedName{Name: "wildwest-provider-oidc"}, binding))
	assert.Equal(t, "cluster-admin", binding.RoleRef.Name)
	require.Len(t, binding.Subjects, 1)
	assert.Equal(t, rbacv1.Subject{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: "wildwest-abc123:system:serviceaccount:wildwest:controller"}, binding.Subjects[0])

	secret := &corev1.Secret{}
	require.NoError(t, userWs.Get(ctx, types.NamespacedName{Name: "wildwest-provider-kubeconfig", Namespace: "default"}, secret))
	kubeconfig, err := clientcmd.Load(secret.Data["kubeconfig"])
	require.NoError(t, err)
	auth := kubeconfig.AuthInfos[kubeconfig.Contexts[kubeconfig.CurrentContext].AuthInfo]
	assert.Empty(t, auth.Token, "no credential is stored")
	assert.Equal(t, oidcTokenFile, auth.TokenFile)
	cluster := kubeconfig.Clusters[kubeconfig.Contexts[kubeconfig.CurrentContext].Cluster]
	assert.Equal(t, "https://kcp.api.example.com/clusters/wildwest-cluster", cluster.Server)
	assert.Equal(t, []byte("fake-ca"), cluster.CertificateAuthorityData)
}

func TestProcessOIDCWaitsForRootCA(t *testing.T) {
	sub := NewScopedKubeconfigSubroutine(nil, nil, config.KCPConfig{}, "https://kcp.api.example.com", nil)
	ws := &kcptenancyv1alpha1.Workspace{Spec: kcptenancyv1alpha1.WorkspaceSpec{Cluster: "c"}}

	providerWs := newFakeClient(t)

	result, err := sub.processOIDC(newLoggerCtx(t), newOIDCProvider(), ws, providerWs, newFakeClient(t))
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
	require.NoError(t, providerWs.Get(context.Background(), types.NamespacedName{Name: "default"}, &corev1.Namespace{}), "kcp publishes the root CA only into existing namespaces")
}
