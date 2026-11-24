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

package subroutines

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

const (
	scopedTestSecretName = "provider-kubeconfig"
	scopedTestNamespace  = "platform-mesh-system"
)

// newScopedKubeconfigTestEnv returns a context with logger and operator config, the scheme for fake
// clients, and a KcpHelper whose kcp client serves kcpObjects for every workspace.
func newScopedKubeconfigTestEnv(t *testing.T, kcpObjects ...ctrlruntimeclient.Object) (context.Context, *runtime.Scheme, ctrlruntimeclient.Client, *mocks.KcpHelper) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, kcpapisv1alpha1.AddToScheme(scheme))
	require.NoError(t, kcpapisv1alpha2.AddToScheme(scheme))

	kcpClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(kcpObjects...).Build()
	kcpHelper := new(mocks.KcpHelper)
	kcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).Return(kcpClient, nil)

	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)
	operatorCfg := config.OperatorConfig{}
	operatorCfg.KCP.Namespace = scopedTestNamespace
	operatorCfg.KCP.FrontProxyName = "frontproxy"
	operatorCfg.KCP.FrontProxyPort = "6443"
	ctx := context.WithValue(t.Context(), keys.LoggerCtxKey, log)
	ctx = context.WithValue(ctx, keys.ConfigCtxKey, operatorCfg)
	return ctx, scheme, kcpClient, kcpHelper
}

func TestWriteScopedKubeconfigToSecretTokenHandling(t *testing.T) {
	const saUID = "sa-uid"
	now := time.Now()
	validToken := fakeServiceAccountToken(t, now.Add(-time.Hour), now.Add(24*time.Hour), saUID)
	staleToken := fakeServiceAccountToken(t, now.Add(-48*time.Hour), now.Add(time.Hour), saUID)

	tests := []struct {
		name           string
		storedToken    string
		kubeconfigOnly bool
		wantToken      string
	}{
		{name: "no secret yet", storedToken: "", wantToken: "fake-token"},
		{name: "secret without token key is migrated", kubeconfigOnly: true, wantToken: "fake-token"},
		{name: "valid token is kept", storedToken: validToken, wantToken: validToken},
		{name: "token past half-life is replaced", storedToken: staleToken, wantToken: "fake-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, scheme, _, kcpHelper := newScopedKubeconfigTestEnv(t,
				&kcpapisv1alpha2.APIExport{ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"}},
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
					Name: scopedSAPrefix + scopedTestSecretName, Namespace: defaultScopedSANamespace, UID: saUID,
				}},
			)

			k8sBuilder := fake.NewClientBuilder().WithScheme(scheme)
			switch {
			case tt.kubeconfigOnly:
				k8sBuilder = k8sBuilder.WithObjects(&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: scopedTestSecretName, Namespace: scopedTestNamespace},
					Data:       map[string][]byte{scopedKubeconfigSecretKey: []byte("old-kubeconfig")},
				})
			case tt.storedToken != "":
				k8sBuilder = k8sBuilder.WithObjects(&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: scopedTestSecretName, Namespace: scopedTestNamespace},
					Data:       map[string][]byte{scopedTokenSecretKey: []byte(tt.storedToken)},
				})
			}
			k8sClient := k8sBuilder.Build()

			pc := pmcorev1alpha1.ProviderConnection{
				Path:           "root:platform-mesh-system",
				Secret:         scopedTestSecretName,
				APIExportNames: []string{"core.platform-mesh.io"},
			}
			err := writeScopedKubeconfigToSecret(ctx, k8sClient, kcpHelper, &rest.Config{}, &pmcorev1alpha1.PlatformMesh{}, pc)
			require.NoError(t, err)

			secret := &corev1.Secret{}
			require.NoError(t, k8sClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: scopedTestSecretName, Namespace: scopedTestNamespace}, secret))
			require.Equal(t, tt.wantToken, string(secret.Data[scopedTokenSecretKey]))

			kubeconfig, err := clientcmd.Load(secret.Data[scopedKubeconfigSecretKey])
			require.NoError(t, err)
			authInfo := kubeconfig.AuthInfos["default-auth"]
			require.Equal(t, tt.wantToken, authInfo.Token)
			require.Equal(t, scopedTokenSecretKey, authInfo.TokenFile)
		})
	}
}

func TestWriteScopedKubeconfigToSecretReadOnlyRules(t *testing.T) {
	const exportName = "core.platform-mesh.io"
	export := &kcpapisv1alpha2.APIExport{
		ObjectMeta: metav1.ObjectMeta{Name: exportName},
		Spec: kcpapisv1alpha2.APIExportSpec{
			Resources: []kcpapisv1alpha2.ResourceSchema{{Group: "core.platform-mesh.io", Name: "accounts"}},
			PermissionClaims: []kcpapisv1alpha2.PermissionClaim{
				{GroupResource: kcpapisv1alpha2.GroupResource{Resource: "secrets"}, Verbs: []string{"*"}},
			},
		},
	}
	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: exportName},
		Spec: kcpapisv1alpha1.APIExportEndpointSliceSpec{
			APIExport: kcpapisv1alpha1.ExportBindingReference{Path: "root:platform-mesh-system", Name: exportName},
		},
		Status: kcpapisv1alpha1.APIExportEndpointSliceStatus{
			APIExportEndpoints: []kcpapisv1alpha1.APIExportEndpoint{{URL: "https://kcp.example/services/apiexport/abc/" + exportName}},
		},
	}

	tests := []struct {
		name            string
		pc              pmcorev1alpha1.ProviderConnection
		clusterRoleName string
	}{
		{
			name:            "apiExportNames",
			pc:              pmcorev1alpha1.ProviderConnection{APIExportNames: []string{exportName}},
			clusterRoleName: scopedClusterRolePrefix + scopedTestSecretName + "-core-platform-mesh-io",
		},
		{
			name:            "endpointSliceName",
			pc:              pmcorev1alpha1.ProviderConnection{EndpointSliceName: ptr.To(exportName)},
			clusterRoleName: scopedClusterRolePrefix + scopedTestSecretName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, scheme, kcpClient, kcpHelper := newScopedKubeconfigTestEnv(t, export.DeepCopy(), slice.DeepCopy())
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			pc := tt.pc
			pc.Path = "root:platform-mesh-system"
			pc.Secret = scopedTestSecretName
			pc.ReadOnly = true
			require.NoError(t, writeScopedKubeconfigToSecret(ctx, k8sClient, kcpHelper, &rest.Config{}, &pmcorev1alpha1.PlatformMesh{}, pc))

			cr := &rbacv1.ClusterRole{}
			require.NoError(t, kcpClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: tt.clusterRoleName}, cr))
			require.NotEmpty(t, cr.Rules)
			for _, rule := range cr.Rules {
				require.NotContains(t, rule.Resources, "secrets")
				for _, verb := range rule.Verbs {
					require.Contains(t, []string{"get", "list", "watch"}, verb, "rule %+v", rule)
				}
			}
		})
	}
}

func TestProvidersecretProcessRequeuesForScopedTokens(t *testing.T) {
	const namespace = "platform-mesh-system"

	availableKcpResource := func(kind, name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "operator.kcp.io/v1alpha1",
			"kind":       kind,
			"metadata":   map[string]any{"name": name, "namespace": namespace},
			"status": map[string]any{
				"conditions": []any{map[string]any{"type": "Available", "status": "True"}},
			},
		}}
	}
	adminKubeconfig, err := clientcmd.Write(*buildScopedKubeconfig("https://kcp.example", "admin", nil))
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, kcpapisv1alpha2.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		availableKcpResource("RootShard", "root"),
		availableKcpResource("FrontProxy", "frontproxy"),
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "kcp-admin", Namespace: namespace},
			Data:       map[string][]byte{"kubeconfig": adminKubeconfig},
		},
	).Build()
	kcpClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&kcpapisv1alpha2.APIExport{ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"}},
	).Build()
	kcpHelper := new(mocks.KcpHelper)
	kcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).Return(kcpClient, nil)

	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)
	operatorCfg := config.OperatorConfig{}
	operatorCfg.KCP.Namespace = namespace
	operatorCfg.KCP.RootShardName = "root"
	operatorCfg.KCP.FrontProxyName = "frontproxy"
	operatorCfg.KCP.FrontProxyPort = "6443"
	operatorCfg.KCP.ClusterAdminSecretName = "kcp-admin"
	ctx := context.WithValue(t.Context(), keys.LoggerCtxKey, log)
	ctx = context.WithValue(ctx, keys.ConfigCtxKey, operatorCfg)

	instance := &pmcorev1alpha1.PlatformMesh{}
	instance.Spec.Kcp.ProviderConnections = []pmcorev1alpha1.ProviderConnection{{
		Path:           "root:platform-mesh-system",
		Secret:         "provider-kubeconfig",
		APIExportNames: []string{"core.platform-mesh.io"},
	}}

	res, err := NewProviderSecretSubroutine(k8sClient, kcpHelper, fakeHelm{ready: true}, "https://kcp.example").Process(ctx, instance)
	require.NoError(t, err)
	require.True(t, res.IsContinue())
	require.Equal(t, scopedTokenRenewalCheckInterval, res.Requeue())
}
