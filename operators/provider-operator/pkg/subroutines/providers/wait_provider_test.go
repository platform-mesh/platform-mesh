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
	"testing"

	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/provider-operator/internal/config"
	"go.platform-mesh.io/provider-operator/pkg/subroutines/mocks"
)

type WaitProviderTestSuite struct {
	suite.Suite
	testObj       *WaitProviderSubroutine
	clientMock    *mocks.Client
	kcpHelperMock *mocks.KcpHelper
	kcpClientMock *mocks.Client
	log           *logger.Logger
	operatorCfg   config.OperatorConfig
}

func TestWaitProviderTestSuite(t *testing.T) {
	suite.Run(t, new(WaitProviderTestSuite))
}

func (s *WaitProviderTestSuite) SetupTest() {
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "WaitProviderTestSuite"
	s.log, _ = logger.New(cfg)

	s.clientMock = new(mocks.Client)
	s.kcpHelperMock = new(mocks.KcpHelper)
	s.kcpClientMock = new(mocks.Client)

	s.clientMock.EXPECT().Scheme().Return(runtime.NewScheme()).Maybe()

	s.operatorCfg = config.OperatorConfig{}
	s.operatorCfg.KCP.ClusterAdminSecretName = "kcp-admin"
	s.operatorCfg.KCP.Namespace = "platform-mesh-system"

	s.testObj = NewWaitProviderSubroutine(s.clientMock, s.kcpHelperMock, &s.operatorCfg, "https://kcp.api.example.com")
}

func (s *WaitProviderTestSuite) TearDownTest() {
	s.clientMock = nil
	s.kcpHelperMock = nil
	s.kcpClientMock = nil
	s.testObj = nil
}

func (s *WaitProviderTestSuite) newCtx() context.Context {
	return context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
}

func (s *WaitProviderTestSuite) newManagedProvider() *providersv1alpha1.ManagedProvider {
	return &providersv1alpha1.ManagedProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cowboys",
			Namespace: "providers-wildwest-ns",
		},
	}
}

func (s *WaitProviderTestSuite) mockAdminSecret() {
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"ca.crt":  []byte("fake-ca"),
				"tls.crt": []byte("fake-cert"),
				"tls.key": []byte("fake-key"),
			}
			return nil
		})
}

// --- Process ---

// Default providerRefPath = "root:providers:system" (no ProviderReference set)
// Default providerRefName = "cowboys" (inst.Name)

func (s *WaitProviderTestSuite) TestProcess_ProviderNotReady() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys"}, mock.AnythingOfType("*v1alpha1.Provider")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			obj.(*providersv1alpha1.Provider).Status.Phase = "Pending"
			return nil
		})

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseWaitingForProvider, inst.Status.Phase)
}

func (s *WaitProviderTestSuite) TestProcess_ProviderReady() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys"}, mock.AnythingOfType("*v1alpha1.Provider")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			obj.(*providersv1alpha1.Provider).Status.Phase = "Ready"
			return nil
		})

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *WaitProviderTestSuite) TestProcess_CustomProviderReference() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.ProviderReference = &providersv1alpha1.ProviderReferenceSpec{
		Path: "root:custom:cowboys",
		Name: "my-provider",
	}

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:custom:cowboys").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "my-provider"}, mock.AnythingOfType("*v1alpha1.Provider")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			obj.(*providersv1alpha1.Provider).Status.Phase = "Ready"
			return nil
		})

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
	s.kcpHelperMock.AssertExpectations(s.T())
}
