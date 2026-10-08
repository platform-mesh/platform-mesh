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

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

type FeaturesTestSuite struct {
	suite.Suite
	clientMock *mocks.Client
	helperMock *mocks.KcpHelper
	testObj    *FeatureToggleSubroutine
	log        *logger.Logger
}

func TestFeaturesTestSuite(t *testing.T) {
	suite.Run(t, new(FeaturesTestSuite))
}

func (s *FeaturesTestSuite) SetupTest() {
	s.clientMock = new(mocks.Client)
	s.helperMock = new(mocks.KcpHelper)
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "FeaturesTestSuite"
	s.log, _ = logger.New(cfg)
	s.testObj = NewFeatureToggleSubroutine(s.clientMock, s.helperMock, &config.OperatorConfig{
		WorkspaceDir: "../..",
	}, "https://kcp.example.com")
}

func (s *FeaturesTestSuite) TearDownTest() {
	s.clientMock = nil
	s.helperMock = nil
	s.testObj = nil
}

func (s *FeaturesTestSuite) resetFeatureToggleTest() {
	s.clientMock = new(mocks.Client)
	s.helperMock = new(mocks.KcpHelper)
	s.testObj = NewFeatureToggleSubroutine(s.clientMock, s.helperMock, &config.OperatorConfig{
		WorkspaceDir: "../..",
	}, "https://kcp.example.com")
}

// setupFeatureToggleApplyMocks configures clients for one or more
// applyKcpManifests executions
func (s *FeaturesTestSuite) setupFeatureToggleApplyMocks(
	operatorCfg config.OperatorConfig, applyKcpManifestsCalls int,
) *mocks.Client {
	secretGetCount := applyKcpManifestsCalls * 2 // (once in applyKcpManifests, once in buildKubeconfig)
	fakeKubeconfig := []byte(`apiVersion: v1
clusters:
- cluster:
    server: https://kcp.example.com
  name: kcp
contexts:
- context:
    cluster: kcp
    user: admin
  name: kcp
current-context: kcp
kind: Config
users:
- name: admin
  user:
    token: fake-token
`)
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{
			Name:      operatorCfg.KCP.ClusterAdminSecretName,
			Namespace: operatorCfg.KCP.Namespace,
		}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"ca.crt":     []byte("test-ca-data"),
				"tls.crt":    []byte("test-tls-crt"),
				"tls.key":    []byte("test-tls-key"),
				"kubeconfig": fakeKubeconfig,
			}
			return nil
		}).
		Times(secretGetCount)

	mockKcpClient := new(mocks.Client)
	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, mock.Anything).
		Return(mockKcpClient, nil).
		Maybe()
	s.helperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:orgs:default").
		Return(mockKcpClient, nil).
		Maybe()

	s.clientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			unstructuredObj := obj.(*unstructured.Unstructured)
			unstructuredObj.Object = map[string]any{
				"status": map[string]any{
					"phase": "Ready",
					"conditions": []any{
						map[string]any{
							"type":   "Available",
							"status": "True",
						},
					},
				},
			}
			return nil
		}).
		Maybe()

	mockKcpClient.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	mockKcpClient.EXPECT().
		Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			ws := obj.(*kcptenancyv1alpha1.Workspace)
			ws.Status.Phase = "Ready"
			return nil
		}).
		Maybe()

	mockKcpClient.EXPECT().
		Get(mock.Anything, mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(apierrors.NewNotFound(schema.GroupResource{Group: "tenancy.kcp.io", Resource: "workspaces"}, "")).
		Maybe()

	return mockKcpClient
}

func testOperatorCfg() config.OperatorConfig {
	operatorCfg := config.OperatorConfig{}
	operatorCfg.KCP.RootShardName = "root-shard"
	operatorCfg.KCP.FrontProxyName = "front-proxy"
	operatorCfg.KCP.Namespace = "kcp-system"
	operatorCfg.KCP.ClusterAdminSecretName = "kcp-admin-kubeconfig"
	return operatorCfg
}

func (s *FeaturesTestSuite) TestProcess() {
	operatorCfg := testOperatorCfg()
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
	ctx = context.WithValue(ctx, keys.ConfigCtxKey, operatorCfg)

	manifestBackedToggles := []struct {
		name   string
		toggle string
	}{
		{"feature-enable-getting-started", "feature-enable-getting-started"},
		{"feature-accounts-in-accounts", "feature-accounts-in-accounts"},
		{"feature-enable-account-iam-ui", "feature-enable-account-iam-ui"},
		{"feature-enable-terminal-controller-manager", "feature-enable-terminal-controller-manager"},
	}

	for _, tc := range manifestBackedToggles {
		s.Run(tc.name, func() {
			s.resetFeatureToggleTest()
			s.setupFeatureToggleApplyMocks(operatorCfg, 1)

			result, err := s.testObj.Process(ctx, &pmcorev1alpha1.PlatformMesh{
				Spec: pmcorev1alpha1.PlatformMeshSpec{
					FeatureToggles: []pmcorev1alpha1.FeatureToggle{
						{Name: tc.toggle, Parameters: map[string]string{}},
					},
				},
			})
			s.Assert().NoError(err)
			s.Assert().Equal(subroutines.OK(), result)
		})
	}

	s.Run("all manifest-backed toggles in one reconcile", func() {
		s.resetFeatureToggleTest()
		s.setupFeatureToggleApplyMocks(operatorCfg, len(manifestBackedToggles))
		toggles := make([]pmcorev1alpha1.FeatureToggle, 0, len(manifestBackedToggles))
		for _, tc := range manifestBackedToggles {
			toggles = append(toggles, pmcorev1alpha1.FeatureToggle{
				Name: tc.toggle, Parameters: map[string]string{},
			})
		}
		result, err := s.testObj.Process(ctx, &pmcorev1alpha1.PlatformMesh{
			Spec: pmcorev1alpha1.PlatformMeshSpec{FeatureToggles: toggles},
		})
		s.Assert().NoError(err)
		s.Assert().Equal(subroutines.OK(), result)
	})

	s.Run("feature-disable-email-verification", func() {
		s.resetFeatureToggleTest()
		result, err := s.testObj.Process(ctx, &pmcorev1alpha1.PlatformMesh{
			Spec: pmcorev1alpha1.PlatformMeshSpec{
				FeatureToggles: []pmcorev1alpha1.FeatureToggle{
					{Name: "feature-disable-email-verification"},
				},
			},
		})
		s.Assert().NoError(err)
		s.Assert().Equal(subroutines.OK(), result)
	})

	s.Run("unknown feature toggle hits default branch", func() {
		s.resetFeatureToggleTest()
		result, err := s.testObj.Process(ctx, &pmcorev1alpha1.PlatformMesh{
			Spec: pmcorev1alpha1.PlatformMeshSpec{
				FeatureToggles: []pmcorev1alpha1.FeatureToggle{
					{Name: "unknown-toggle-name"},
				},
			},
		})
		s.Assert().NoError(err)
		s.Assert().Equal(subroutines.OK(), result)
	})
}
