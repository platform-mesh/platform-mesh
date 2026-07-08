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

	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/provider-operator/internal/config"
	"go.platform-mesh.io/provider-operator/pkg/subroutines/mocks"
)

type WorkspaceTestSuite struct {
	suite.Suite
	testObj       *ProviderWorkspaceSubroutine
	clientMock    *mocks.Client
	kcpHelperMock *mocks.KcpHelper
	kcpClientMock *mocks.Client
	log           *logger.Logger
	kcpCfg        config.KCPConfig
}

func TestWorkspaceTestSuite(t *testing.T) {
	suite.Run(t, new(WorkspaceTestSuite))
}

func (s *WorkspaceTestSuite) SetupTest() {
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "WorkspaceTestSuite"
	s.log, _ = logger.New(cfg)

	s.clientMock = new(mocks.Client)
	s.kcpHelperMock = new(mocks.KcpHelper)
	s.kcpClientMock = new(mocks.Client)

	s.clientMock.EXPECT().Scheme().Return(runtime.NewScheme()).Maybe()
	s.kcpClientMock.EXPECT().Scheme().Return(runtime.NewScheme()).Maybe()

	s.kcpCfg = config.KCPConfig{
		ClusterAdminSecretName: "kcp-admin",
		Namespace:              "platform-mesh-system",
	}

	var err error
	s.testObj, err = NewProviderWorkspaceSubroutine(s.clientMock, s.kcpHelperMock, s.kcpCfg, "https://kcp.api.example.com")
	s.Require().NoError(err)
}

func (s *WorkspaceTestSuite) TearDownTest() {
	s.clientMock = nil
	s.kcpHelperMock = nil
	s.kcpClientMock = nil
	s.testObj = nil
}

func (s *WorkspaceTestSuite) newCtx() context.Context {
	return context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
}

func (s *WorkspaceTestSuite) newProvider() *providersv1alpha1.Provider {
	return &providersv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name: "wildwest",
			Annotations: map[string]string{
				"kcp.io/cluster": "abc123",
			},
		},
	}
}

func (s *WorkspaceTestSuite) mockAdminSecret() {
	s.clientMock.EXPECT().
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

// --- Process ---

func (s *WorkspaceTestSuite) TestProcess() {
	// wsName = "wildwest-abc123" (provider.Name + "-" + annotation["kcp.io/cluster"])
	wsName := "wildwest-abc123"

	cases := []struct {
		name            string
		setup           func()
		wantErrContains string
		wantRequeue     bool
		wantPhase       string
	}{
		{
			name: "build kcp admin config fails",
			setup: func() {
				s.clientMock.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
					Return(errors.New("connection refused"))
			},
			wantErrContains: "failed to build kcp admin config",
		},
		{
			name: "new kcp client fails",
			setup: func() {
				s.mockAdminSecret()
				s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
					Return(nil, errors.New("dial error"))
			},
			wantErrContains: "failed to create kcp client",
		},
		{
			name: "ensure workspace fails",
			setup: func() {
				s.mockAdminSecret()
				s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
					Return(s.kcpClientMock, nil)
				// CreateOrUpdate: Get → NotFound → Create fails
				s.kcpClientMock.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: wsName}, mock.AnythingOfType("*v1alpha1.Workspace")).
					Return(kerrors.NewNotFound(schema.GroupResource{Resource: "workspaces"}, wsName))
				s.kcpClientMock.EXPECT().
					Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace"), mock.Anything).
					Return(errors.New("create failed"))
			},
			wantErrContains: "create failed",
		},
		{
			name: "readiness get fails",
			setup: func() {
				s.mockAdminSecret()
				s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
					Return(s.kcpClientMock, nil)
				// CreateOrUpdate: Get → NotFound → Create succeeds
				s.kcpClientMock.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: wsName}, mock.AnythingOfType("*v1alpha1.Workspace")).
					Return(kerrors.NewNotFound(schema.GroupResource{Resource: "workspaces"}, wsName)).Once()
				s.kcpClientMock.EXPECT().
					Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace"), mock.Anything).
					Return(nil)
				// Readiness Get fails
				s.kcpClientMock.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: wsName}, mock.AnythingOfType("*v1alpha1.Workspace")).
					Return(errors.New("api server error"))
			},
			wantErrContains: "failed to get workspace",
		},
		{
			name: "workspace not ready - requeue",
			setup: func() {
				s.mockAdminSecret()
				s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
					Return(s.kcpClientMock, nil)
				// CreateOrUpdate: Get → NotFound → Create succeeds
				s.kcpClientMock.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: wsName}, mock.AnythingOfType("*v1alpha1.Workspace")).
					Return(kerrors.NewNotFound(schema.GroupResource{Resource: "workspaces"}, wsName)).Once()
				s.kcpClientMock.EXPECT().
					Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace"), mock.Anything).
					Return(nil)
				// Readiness Get: workspace not ready
				s.kcpClientMock.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: wsName}, mock.AnythingOfType("*v1alpha1.Workspace")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						ws := obj.(*kcptenancyv1alpha.Workspace)
						ws.Status.Phase = ""
						return nil
					})
			},
			wantRequeue: true,
			wantPhase:   providersv1alpha1.ProviderPhaseProvisioningWorkspace,
		},
		{
			name: "happy path workspace ready",
			setup: func() {
				s.mockAdminSecret()
				s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
					Return(s.kcpClientMock, nil)
				// CreateOrUpdate: Get → NotFound → Create succeeds
				s.kcpClientMock.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: wsName}, mock.AnythingOfType("*v1alpha1.Workspace")).
					Return(kerrors.NewNotFound(schema.GroupResource{Resource: "workspaces"}, wsName)).Once()
				s.kcpClientMock.EXPECT().
					Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace"), mock.Anything).
					Return(nil)
				// Readiness Get: workspace Ready
				s.kcpClientMock.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: wsName}, mock.AnythingOfType("*v1alpha1.Workspace")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						ws := obj.(*kcptenancyv1alpha.Workspace)
						ws.Status.Phase = "Ready"
						return nil
					})
			},
			wantPhase: providersv1alpha1.ProviderPhasePending,
		},
	}

	for _, tc := range cases {
		tc := tc
		s.Run(tc.name, func() {
			s.SetupTest()
			inst := s.newProvider()
			if tc.setup != nil {
				tc.setup()
			}

			result, err := s.testObj.Process(s.newCtx(), inst)

			if tc.wantErrContains != "" {
				s.Require().Error(err)
				s.Assert().Contains(err.Error(), tc.wantErrContains)
				s.Assert().True(result.IsContinue())
			} else if tc.wantRequeue {
				s.Require().NoError(err)
				s.Assert().True(result.IsStopWithRequeue())
			} else {
				s.Require().NoError(err)
				s.Assert().True(result.IsContinue())
			}
			if tc.wantPhase != "" {
				s.Assert().Equal(tc.wantPhase, inst.Status.Phase)
			}
		})
	}
}

// --- Finalize ---

func (s *WorkspaceTestSuite) TestFinalize() {
	wsName := "wildwest-abc123"

	cases := []struct {
		name            string
		setup           func()
		wantErrContains string
		wantRequeue     bool
	}{
		{
			name: "build kcp admin config fails",
			setup: func() {
				s.clientMock.EXPECT().
					Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
					Return(errors.New("connection refused"))
			},
			wantErrContains: "failed to build kcp admin config",
		},
		{
			name: "new kcp client fails",
			setup: func() {
				s.mockAdminSecret()
				s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
					Return(nil, errors.New("dial error"))
			},
			wantErrContains: "failed to create kcp client",
		},
		{
			name: "delete fails",
			setup: func() {
				s.mockAdminSecret()
				s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
					Return(s.kcpClientMock, nil)
				s.kcpClientMock.EXPECT().
					Delete(mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace"), mock.Anything).
					Return(errors.New("delete failed"))
			},
			wantErrContains: "failed to delete provider workspace",
		},
		{
			name: "workspace already gone",
			setup: func() {
				s.mockAdminSecret()
				s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
					Return(s.kcpClientMock, nil)
				s.kcpClientMock.EXPECT().
					Delete(mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace"), mock.Anything).
					Return(kerrors.NewNotFound(schema.GroupResource{Resource: "workspaces"}, wsName))
			},
		},
		{
			name: "delete accepted - wait for deletion",
			setup: func() {
				s.mockAdminSecret()
				s.kcpHelperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").
					Return(s.kcpClientMock, nil)
				s.kcpClientMock.EXPECT().
					Delete(mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace"), mock.Anything).
					Return(nil)
			},
			wantRequeue: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		s.Run(tc.name, func() {
			s.SetupTest()
			inst := s.newProvider()
			if tc.setup != nil {
				tc.setup()
			}

			result, err := s.testObj.Finalize(s.newCtx(), inst)

			if tc.wantErrContains != "" {
				s.Require().Error(err)
				s.Assert().Contains(err.Error(), tc.wantErrContains)
				s.Assert().True(result.IsContinue())
			} else if tc.wantRequeue {
				s.Require().NoError(err)
				s.Assert().True(result.IsStopWithRequeue())
			} else {
				s.Require().NoError(err)
				s.Assert().True(result.IsContinue())
			}
		})
	}
}
