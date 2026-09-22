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
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcpapiv1alpha2 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha2"
)

func TestWriteScopedKubeconfigToSecretTokenHandling(t *testing.T) {
	const (
		secretName = "provider-kubeconfig"
		namespace  = "platform-mesh-system"
		saUID      = "sa-uid"
	)
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
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, rbacv1.AddToScheme(scheme))
			require.NoError(t, kcpapiv1alpha2.AddToScheme(scheme))

			k8sBuilder := fake.NewClientBuilder().WithScheme(scheme)
			switch {
			case tt.kubeconfigOnly:
				k8sBuilder = k8sBuilder.WithObjects(&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
					Data:       map[string][]byte{scopedKubeconfigSecretKey: []byte("old-kubeconfig")},
				})
			case tt.storedToken != "":
				k8sBuilder = k8sBuilder.WithObjects(&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
					Data:       map[string][]byte{scopedTokenSecretKey: []byte(tt.storedToken)},
				})
			}
			k8sClient := k8sBuilder.Build()
			kcpClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				&kcpapiv1alpha2.APIExport{ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"}},
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
					Name: scopedSAPrefix + secretName, Namespace: defaultScopedSANamespace, UID: saUID,
				}},
			).Build()
			kcpHelper := new(mocks.KcpHelper)
			kcpHelper.EXPECT().NewKcpClient(mock.Anything, mock.Anything).Return(kcpClient, nil)

			log, err := logger.New(logger.DefaultConfig())
			require.NoError(t, err)
			operatorCfg := config.OperatorConfig{}
			operatorCfg.KCP.Namespace = namespace
			operatorCfg.KCP.FrontProxyName = "frontproxy"
			operatorCfg.KCP.FrontProxyPort = "6443"
			ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, log)
			ctx = context.WithValue(ctx, keys.ConfigCtxKey, operatorCfg)

			pc := pmcorev1alpha1.ProviderConnection{
				Path:           "root:platform-mesh-system",
				Secret:         secretName,
				APIExportNames: []string{"core.platform-mesh.io"},
			}
			err = writeScopedKubeconfigToSecret(ctx, k8sClient, kcpHelper, &rest.Config{}, &pmcorev1alpha1.PlatformMesh{}, pc)
			require.NoError(t, err)

			secret := &corev1.Secret{}
			require.NoError(t, k8sClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: secretName, Namespace: namespace}, secret))
			require.Equal(t, tt.wantToken, string(secret.Data[scopedTokenSecretKey]))

			kubeconfig, err := clientcmd.Load(secret.Data[scopedKubeconfigSecretKey])
			require.NoError(t, err)
			authInfo := kubeconfig.AuthInfos["default-auth"]
			require.Equal(t, tt.wantToken, authInfo.Token)
			require.Equal(t, scopedTokenSecretKey, authInfo.TokenFile)
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
	require.NoError(t, kcpapiv1alpha2.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		availableKcpResource("RootShard", "root"),
		availableKcpResource("FrontProxy", "frontproxy"),
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "kcp-admin", Namespace: namespace},
			Data:       map[string][]byte{"kubeconfig": adminKubeconfig},
		},
	).Build()
	kcpClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&kcpapiv1alpha2.APIExport{ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"}},
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
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, log)
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
