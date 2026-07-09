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
	"os"
	"testing"

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

	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/provider-operator/internal/config"
	"go.platform-mesh.io/provider-operator/pkg/subroutines/mocks"
)

var secretKubeconfigData, _ = os.ReadFile("../test/kubeconfig.yaml")

type KubeconfigCopyTestSuite struct {
	suite.Suite
	testObj       *KubeconfigCopySubroutine
	clientMock    *mocks.Client
	kcpHelperMock *mocks.KcpHelper
	kcpClientMock *mocks.Client
	scheme        *runtime.Scheme
	log           *logger.Logger
	operatorCfg   config.OperatorConfig
}

func TestKubeconfigCopyTestSuite(t *testing.T) {
	suite.Run(t, new(KubeconfigCopyTestSuite))
}

func (s *KubeconfigCopyTestSuite) SetupTest() {
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "KubeconfigCopyTestSuite"
	s.log, _ = logger.New(cfg)

	s.clientMock = new(mocks.Client)
	s.kcpHelperMock = new(mocks.KcpHelper)
	s.kcpClientMock = new(mocks.Client)

	s.scheme = runtime.NewScheme()
	s.clientMock.EXPECT().Scheme().Return(s.scheme).Maybe()

	s.operatorCfg = config.OperatorConfig{}
	s.operatorCfg.KCP.ClusterAdminSecretName = "kcp-admin"
	s.operatorCfg.KCP.Namespace = "platform-mesh-system"

	s.testObj = NewKubeconfigCopySubroutine(s.clientMock, s.kcpHelperMock, &s.operatorCfg, "https://kcp.api.example.com")
}

func (s *KubeconfigCopyTestSuite) TearDownTest() {
	s.clientMock = nil
	s.kcpHelperMock = nil
	s.kcpClientMock = nil
	s.testObj = nil
}

func (s *KubeconfigCopyTestSuite) newCtx() context.Context {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	return context.WithValue(ctx, keys.ConfigCtxKey, s.operatorCfg)
}

func (s *KubeconfigCopyTestSuite) newManagedProvider() *providersv1alpha1.ManagedProvider {
	return &providersv1alpha1.ManagedProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cowboys",
			Namespace: "providers-wildwest-ns",
		},
	}
}

func (s *KubeconfigCopyTestSuite) mockAdminSecret() {
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Name = "kcp-admin"
			secret.Namespace = "platform-mesh-system"
			secret.Data = map[string][]byte{
				"ca.crt":  []byte("fake-ca"),
				"tls.crt": []byte("fake-cert"),
				"tls.key": []byte("fake-key"),
			}
			return nil
		})
}

func (s *KubeconfigCopyTestSuite) mockProviderWithSecretRef() {
	// Default providerRefName = "cowboys" (inst.Name), providerRefPath = "root:providers:system"
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys"}, mock.AnythingOfType("*v1alpha1.Provider")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
			provider := obj.(*providersv1alpha1.Provider)
			provider.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
				Name:      "cowboys-kubeconfig",
				Namespace: "kcp-side-ns",
			}
			// Must match providerKubeconfigSecretSpec("cowboys", "providers-wildwest-ns", nil)
			provider.Spec.ProviderKubeconfigSecret = &providersv1alpha1.KubeconfigSecretSpec{
				Name:      "cowboys-provider-kubeconfig",
				Namespace: "providers-wildwest-ns",
				Key:       "kubeconfig",
			}
			return nil
		})
}

func (s *KubeconfigCopyTestSuite) TestProcess_BuildKcpAdminConfigFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
		Return(errors.New("connection refused"))

	result, err := s.testObj.Process(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to build kcp admin config")
}

func (s *KubeconfigCopyTestSuite) TestProcess_NewKcpClientFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	// default providerRefPath = "root:providers:system"
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(nil, errors.New("dial error"))

	result, err := s.testObj.Process(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to create kcp client")
}

func (s *KubeconfigCopyTestSuite) TestProcess_KubeconfigSecretRefNotSetYet() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys"}, mock.AnythingOfType("*v1alpha1.Provider")).
		Return(nil) // Provider found, Status.ProviderKubeconfigSecretRef is nil

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseCopyingKubeconfig, inst.Status.Phase)
}

func (s *KubeconfigCopyTestSuite) TestProcess_KubeconfigSecretGetFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.mockProviderWithSecretRef()
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys-kubeconfig", Namespace: "kcp-side-ns"}, mock.AnythingOfType("*v1.Secret")).
		Return(errors.New("secret fetch failed"))

	result, err := s.testObj.Process(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
}

func (s *KubeconfigCopyTestSuite) TestProcess_HappyPath() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.mockProviderWithSecretRef()
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys-kubeconfig", Namespace: "kcp-side-ns"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{"kubeconfig": secretKubeconfigData}
			return nil
		})
	// Ensure namespace "default" in runtime cluster (r.client, since RuntimeKubeconfigSecretName is empty)
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Namespace"), mock.Anything).
		Return(nil)
	// Default copy destination: Name = providerKubeconfigSecretName(inst.Name), Namespace = inst.Namespace
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys-provider-kubeconfig", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*v1.Secret")).
		Return(kerrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "cowboys-provider-kubeconfig"))
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(nil)

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
	s.Require().NotNil(inst.Status.ProviderKubeconfigSecretRef)
	s.Assert().Equal("cowboys-provider-kubeconfig", inst.Status.ProviderKubeconfigSecretRef.Name)
	s.Assert().Equal("providers-wildwest-ns", inst.Status.ProviderKubeconfigSecretRef.Namespace)
}

func (s *KubeconfigCopyTestSuite) TestProcess_CustomProviderReference() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.ProviderReference = &providersv1alpha1.ProviderReferenceSpec{
		Path: "root:custom:path",
		Name: "my-provider",
	}

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:custom:path").
		Return(nil, errors.New("stop here"))

	_, _ = s.testObj.Process(ctx, inst)

	s.kcpHelperMock.AssertExpectations(s.T())
}

func (s *KubeconfigCopyTestSuite) TestFinalize_NilKubeconfigRef() {
	// No ProviderKubeconfigSecretRef set (provider never reached Ready) → no-op.
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *KubeconfigCopyTestSuite) TestFinalize_DeletesSecret() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
		Name:      "cowboys-provider-kubeconfig",
		Namespace: inst.Namespace,
	}

	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(nil)

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *KubeconfigCopyTestSuite) TestFinalize_SecretNotFound() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
		Name:      "cowboys-provider-kubeconfig",
		Namespace: inst.Namespace,
	}

	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(kerrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "cowboys-provider-kubeconfig"))

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *KubeconfigCopyTestSuite) TestFinalize_DeleteError() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Status.ProviderKubeconfigSecretRef = &corev1.SecretReference{
		Name:      "cowboys-provider-kubeconfig",
		Namespace: inst.Namespace,
	}

	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
		Return(errors.New("delete failed"))

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
}
