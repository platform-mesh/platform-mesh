/*
Copyright 2026.

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
	"errors"
	"testing"

	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/provider-operator/internal/config"
	"go.platform-mesh.io/provider-operator/pkg/subroutines/mocks"
)

type ScopedKubeconfigTestSuite struct {
	suite.Suite
	testObj         *ScopedKubeconfigSubroutine
	clMock          *mocks.Client // VW cluster client
	localClientMock *mocks.Client // admin secret reader
	kcpHelperMock   *mocks.KcpHelper
	kcpClientMock   *mocks.Client // root:providers scoped client
	wsClientMock    *mocks.Client // provider workspace scoped client
	log             *logger.Logger
	kcpCfg          config.KCPConfig
}

func TestScopedKubeconfigTestSuite(t *testing.T) {
	suite.Run(t, new(ScopedKubeconfigTestSuite))
}

func (s *ScopedKubeconfigTestSuite) SetupTest() {
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "ScopedKubeconfigTestSuite"
	s.log, _ = logger.New(cfg)

	s.clMock = new(mocks.Client)
	s.localClientMock = new(mocks.Client)
	s.kcpHelperMock = new(mocks.KcpHelper)
	s.kcpClientMock = new(mocks.Client)
	s.wsClientMock = new(mocks.Client)

	s.clMock.EXPECT().Scheme().Return(runtime.NewScheme()).Maybe()
	s.localClientMock.EXPECT().Scheme().Return(runtime.NewScheme()).Maybe()
	s.kcpClientMock.EXPECT().Scheme().Return(runtime.NewScheme()).Maybe()
	s.wsClientMock.EXPECT().Scheme().Return(runtime.NewScheme()).Maybe()

	s.kcpCfg = config.KCPConfig{
		ClusterAdminSecretName: "kcp-admin",
		Namespace:              "platform-mesh-system",
	}

	s.testObj = NewScopedKubeconfigSubroutine(
		s.localClientMock,
		s.kcpHelperMock,
		s.kcpCfg,
		"https://kcp.api.example.com",
		func(_ context.Context) (client.Client, error) {
			return s.clMock, nil
		},
	)
}

func (s *ScopedKubeconfigTestSuite) TearDownTest() {
	s.clMock = nil
	s.localClientMock = nil
	s.kcpHelperMock = nil
	s.kcpClientMock = nil
	s.wsClientMock = nil
	s.testObj = nil
}

func (s *ScopedKubeconfigTestSuite) newCtx() context.Context {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	return mccontext.WithCluster(ctx, multicluster.ClusterName("root:providers:wildwest"))
}

func (s *ScopedKubeconfigTestSuite) newProvider() *providersv1alpha1.Provider {
	return &providersv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name: "wildwest",
			Annotations: map[string]string{
				"kcp.io/cluster": "abc123",
			},
		},
	}
}

func (s *ScopedKubeconfigTestSuite) mockAdminSecret() {
	s.localClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"ca.crt":  []byte("fake-ca"),
				"tls.crt": []byte("fake-cert"),
				"tls.key": []byte("fake-key"),
			}
			return nil
		})
}

func (s *ScopedKubeconfigTestSuite) mockWorkspaceReady() {
	// wsName for "wildwest" with annotation "kcp.io/cluster"="abc123" is "wildwest-abc123"
	s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-abc123"}, mock.AnythingOfType("*v1alpha1.Workspace")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			ws := obj.(*kcptenancyv1alpha.Workspace)
			ws.Status.Phase = "Ready"
			return nil
		})
	s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest-abc123").
		Return(s.wsClientMock, nil)
}

// mockPreTokenSteps sets up successful mocks for namespace, SA, and
// ClusterRoleBinding — everything that runs before the token Secret CreateOrUpdate.
func (s *ScopedKubeconfigTestSuite) mockPreTokenSteps() {
	s.mockAdminSecret()
	s.mockWorkspaceReady()

	s.wsClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Namespace"), mock.Anything).
		Return(nil)
	s.wsClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.ServiceAccount"), mock.Anything).
		Return(nil)
	s.wsClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-provider"}, mock.AnythingOfType("*v1.ClusterRoleBinding")).
		Return(kerrors.NewNotFound(schema.GroupResource{Resource: "clusterrolebindings"}, "wildwest-provider"))
	s.wsClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.ClusterRoleBinding"), mock.Anything).
		Return(nil)
}

// --- Process ---

func (s *ScopedKubeconfigTestSuite) TestProcess_BuildKcpAdminConfigFails() {
	s.localClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
		Return(errors.New("connection refused"))

	result, err := s.testObj.Process(s.newCtx(), s.newProvider())

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to build kcp admin config")
}

func (s *ScopedKubeconfigTestSuite) TestProcess_NewKcpClientForProvidersFails() {
	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
		Return(nil, errors.New("dial error"))

	result, err := s.testObj.Process(s.newCtx(), s.newProvider())

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to create kcp client for root:providers")
}

func (s *ScopedKubeconfigTestSuite) TestProcess_WorkspaceNotFound_Requeue() {
	inst := s.newProvider()
	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-abc123"}, mock.AnythingOfType("*v1alpha1.Workspace")).
		Return(kerrors.NewNotFound(schema.GroupResource{Resource: "workspaces"}, "wildwest-abc123"))

	result, err := s.testObj.Process(s.newCtx(), inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ProviderPhaseProvisioningWorkspace, inst.Status.Phase)
}

func (s *ScopedKubeconfigTestSuite) TestProcess_WorkspaceNotReady_Requeue() {
	inst := s.newProvider()
	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-abc123"}, mock.AnythingOfType("*v1alpha1.Workspace")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			ws := obj.(*kcptenancyv1alpha.Workspace)
			ws.Status.Phase = "Initializing"
			return nil
		})

	result, err := s.testObj.Process(s.newCtx(), inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ProviderPhaseProvisioningWorkspace, inst.Status.Phase)
}

func (s *ScopedKubeconfigTestSuite) TestProcess_NamespaceCreateFails() {
	s.mockAdminSecret()
	s.mockWorkspaceReady()
	s.wsClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Namespace"), mock.Anything).
		Return(errors.New("server error"))

	result, err := s.testObj.Process(s.newCtx(), s.newProvider())

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "ensure namespace")
}

func (s *ScopedKubeconfigTestSuite) TestProcess_NamespaceAlreadyExists_Continues() {
	s.mockAdminSecret()
	s.mockWorkspaceReady()
	s.wsClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Namespace"), mock.Anything).
		Return(kerrors.NewAlreadyExists(schema.GroupResource{Resource: "namespaces"}, "default"))
	s.wsClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.ServiceAccount"), mock.Anything).
		Return(errors.New("stop here"))

	_, err := s.testObj.Process(s.newCtx(), s.newProvider())

	// AlreadyExists on namespace is ignored; execution continues to SA creation.
	s.Require().Error(err)
	s.Assert().Contains(err.Error(), "create ServiceAccount")
}

func (s *ScopedKubeconfigTestSuite) TestProcess_SACreateFails() {
	s.mockAdminSecret()
	s.mockWorkspaceReady()
	s.wsClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Namespace"), mock.Anything).
		Return(nil)
	s.wsClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.ServiceAccount"), mock.Anything).
		Return(errors.New("forbidden"))

	result, err := s.testObj.Process(s.newCtx(), s.newProvider())

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "create ServiceAccount")
}

func (s *ScopedKubeconfigTestSuite) TestProcess_TokenNotPopulatedYet() {
	s.mockPreTokenSteps()
	// Token secret Create succeeds but leaves Data empty (no SA token controller).
	s.wsClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-provider-token", Namespace: "default"}, mock.AnythingOfType("*v1.Secret")).
		Return(kerrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "wildwest-provider-token"))
	s.wsClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(nil)

	result, err := s.testObj.Process(s.newCtx(), s.newProvider())

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
}

func (s *ScopedKubeconfigTestSuite) TestProcess_HappyPath() {
	inst := s.newProvider()
	s.mockPreTokenSteps()
	// Token secret: already exists with token data populated → Update.
	s.wsClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-provider-token", Namespace: "default"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.ResourceVersion = "1"
			secret.Data = map[string][]byte{
				"token":  []byte("fake-token"),
				"ca.crt": []byte("fake-ca"),
			}
			return nil
		})
	s.wsClientMock.EXPECT().
		Update(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(nil)
	// Ensure namespace "default" in user's workspace before writing kubeconfig Secret.
	s.clMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Namespace"), mock.Anything).
		Return(nil)
	// Default kubeconfig secret name: "wildwest-provider-kubeconfig"
	s.clMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-provider-kubeconfig", Namespace: "default"}, mock.AnythingOfType("*v1.Secret")).
		Return(kerrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "wildwest-provider-kubeconfig"))
	s.clMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(nil)

	result, err := s.testObj.Process(s.newCtx(), inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
	s.Require().NotNil(inst.Status.ProviderKubeconfigSecretRef)
	s.Assert().Equal("wildwest-provider-kubeconfig", inst.Status.ProviderKubeconfigSecretRef.Name)
	s.Assert().Equal("default", inst.Status.ProviderKubeconfigSecretRef.Namespace)
	s.Assert().Equal(providersv1alpha1.ProviderPhaseReady, inst.Status.Phase)
}

func (s *ScopedKubeconfigTestSuite) TestProcess_HostOverride() {
	inst := s.newProvider()
	inst.Spec.HostOverride = "https://custom.kcp.host"

	s.mockPreTokenSteps()
	s.wsClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-provider-token", Namespace: "default"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.ResourceVersion = "1"
			secret.Data = map[string][]byte{"token": []byte("t"), "ca.crt": []byte("ca")}
			return nil
		})
	s.wsClientMock.EXPECT().Update(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).Return(nil)
	s.clMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Namespace"), mock.Anything).
		Return(nil)
	s.clMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-provider-kubeconfig", Namespace: "default"}, mock.AnythingOfType("*v1.Secret")).
		Return(kerrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "wildwest-provider-kubeconfig"))

	var capturedSecret *corev1.Secret
	s.clMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		RunAndReturn(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
			capturedSecret = obj.(*corev1.Secret)
			return nil
		})

	_, err := s.testObj.Process(s.newCtx(), inst)

	s.Require().NoError(err)
	s.Require().NotNil(capturedSecret)
	s.Assert().Contains(string(capturedSecret.Data["kubeconfig"]), "https://custom.kcp.host")
}

// --- Finalize ---

func (s *ScopedKubeconfigTestSuite) TestFinalize_DeleteFails() {
	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest-abc123").
		Return(s.wsClientMock, nil)
	s.wsClientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1.ClusterRoleBinding"), mock.Anything).
		Return(errors.New("delete failed"))

	result, err := s.testObj.Finalize(s.newCtx(), s.newProvider())

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
}

func (s *ScopedKubeconfigTestSuite) TestFinalize_NotFoundIgnored() {
	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest-abc123").
		Return(s.wsClientMock, nil)
	notFound := kerrors.NewNotFound(schema.GroupResource{Resource: "test"}, "wildwest")
	s.wsClientMock.EXPECT().
		Delete(mock.Anything, mock.Anything, mock.Anything).
		Return(notFound).Times(3)

	result, err := s.testObj.Finalize(s.newCtx(), s.newProvider())

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *ScopedKubeconfigTestSuite) TestFinalize_HappyPath() {
	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest-abc123").
		Return(s.wsClientMock, nil)
	s.wsClientMock.EXPECT().
		Delete(mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Times(3)

	result, err := s.testObj.Finalize(s.newCtx(), s.newProvider())

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *ScopedKubeconfigTestSuite) TestFinalize_WithKubeconfigSecretRef() {
	inst := s.newProvider()
	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
		Name:      "wildwest-provider-kubeconfig",
		Namespace: "default",
	}

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest-abc123").
		Return(s.wsClientMock, nil)
	s.wsClientMock.EXPECT().
		Delete(mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Times(3)
	s.clMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(nil)

	result, err := s.testObj.Finalize(s.newCtx(), inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}
