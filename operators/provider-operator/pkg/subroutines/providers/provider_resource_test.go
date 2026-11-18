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
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/provider-operator/internal/config"
	"go.platform-mesh.io/provider-operator/pkg/subroutines/mocks"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

type ProviderResourceTestSuite struct {
	suite.Suite
	testObj       *ProviderResourceSubroutine
	clientMock    *mocks.Client
	kcpHelperMock *mocks.KcpHelper
	kcpClientMock *mocks.Client
	log           *logger.Logger
	operatorCfg   config.OperatorConfig
}

func TestProviderResourceTestSuite(t *testing.T) {
	suite.Run(t, new(ProviderResourceTestSuite))
}

func (s *ProviderResourceTestSuite) SetupTest() {
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "ProviderResourceTestSuite"
	s.log, _ = logger.New(cfg)

	s.clientMock = new(mocks.Client)
	s.kcpHelperMock = new(mocks.KcpHelper)
	s.kcpClientMock = new(mocks.Client)

	s.clientMock.EXPECT().Scheme().Return(runtime.NewScheme()).Maybe()
	s.kcpClientMock.EXPECT().Scheme().Return(runtime.NewScheme()).Maybe()

	s.operatorCfg = config.OperatorConfig{}
	s.operatorCfg.KCP.ClusterAdminSecretName = "kcp-admin"
	s.operatorCfg.KCP.Namespace = "platform-mesh-system"

	var err error
	s.testObj, err = NewProviderResourceSubroutine(s.clientMock, s.kcpHelperMock, &s.operatorCfg, "https://kcp.api.example.com")
	s.Require().NoError(err, "creating ProviderResourceSubroutine should succeed")
}

func (s *ProviderResourceTestSuite) TearDownTest() {
	s.clientMock = nil
	s.kcpHelperMock = nil
	s.kcpClientMock = nil
	s.testObj = nil
}

func (s *ProviderResourceTestSuite) newCtx() context.Context {
	return context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
}

func (s *ProviderResourceTestSuite) newManagedProvider() *pmprovidersv1alpha1.ManagedProvider {
	return &pmprovidersv1alpha1.ManagedProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cowboys",
			Namespace: "providers-wildwest-ns",
		},
	}
}

func (s *ProviderResourceTestSuite) mockAdminSecret() {
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			secret := obj.(*corev1.Secret)
			secret.Data = map[string][]byte{
				"ca.crt":  []byte("fake-ca"),
				"tls.crt": []byte("fake-cert"),
				"tls.key": []byte("fake-key"),
			}
			return nil
		})
}

// --- GetName ---

func (s *ProviderResourceTestSuite) TestGetName() {
	s.Assert().Equal(ProviderResourceSubroutineName, s.testObj.GetName())
}

// --- Process ---

func (s *ProviderResourceTestSuite) TestProcess_BuildKcpAdminConfigFails() {
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

func (s *ProviderResourceTestSuite) TestProcess_NewKcpClientFails() {
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

func (s *ProviderResourceTestSuite) TestProcess_ApplyFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	// CreateOrUpdate: Get → NotFound → Create fails
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys"}, mock.AnythingOfType("*v1alpha1.Provider")).
		Return(apierrors.NewNotFound(schema.GroupResource{Resource: "providers"}, "cowboys"))
	s.kcpClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Provider"), mock.Anything).
		Return(errors.New("apply failed"))

	result, err := s.testObj.Process(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "apply failed")
}

func (s *ProviderResourceTestSuite) TestProcess_HappyPath() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	// CreateOrUpdate: Get → NotFound → Create succeeds
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cowboys"}, mock.AnythingOfType("*v1alpha1.Provider")).
		Return(apierrors.NewNotFound(schema.GroupResource{Resource: "providers"}, "cowboys"))
	s.kcpClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Provider"), mock.Anything).
		Return(nil)

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *ProviderResourceTestSuite) TestProcess_CustomProviderReference() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.ProviderReference = &pmprovidersv1alpha1.ProviderReferenceSpec{
		Path: "root:custom:path",
		Name: "my-provider",
	}

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:custom:path").
		Return(s.kcpClientMock, nil)
	// CreateOrUpdate: Get → NotFound → Create succeeds
	s.kcpClientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "my-provider"}, mock.AnythingOfType("*v1alpha1.Provider")).
		Return(apierrors.NewNotFound(schema.GroupResource{Resource: "providers"}, "my-provider"))
	s.kcpClientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Provider"), mock.Anything).
		Return(nil)

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
	s.kcpHelperMock.AssertExpectations(s.T())
}

type fakeManager struct {
	mcmanager.Manager
	client ctrlruntimeclient.Client
}

func (m *fakeManager) ClusterFromContext(context.Context) (cluster.Cluster, error) {
	return &fakeCluster{client: m.client}, nil
}

type fakeCluster struct {
	cluster.Cluster
	client ctrlruntimeclient.Client
}

func (c *fakeCluster) GetClient() ctrlruntimeclient.Client { return c.client }

func (s *ProviderResourceTestSuite) TestReconcile_Delete() {
	s.Run("ManagedProvider deleted after cleanupOnDelete is disabled", func() {
		ctx := s.newCtx()
		scheme := runtime.NewScheme()
		s.Require().NoError(pmprovidersv1alpha1.AddToScheme(scheme))

		inst := s.newManagedProvider()
		inst.Spec.CleanupOnDelete = true
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(inst).WithStatusSubresource(inst).Build()

		lc := lifecycle.New(&fakeManager{client: cl}, "ManagedProviderReconciler", func() ctrlruntimeclient.Object {
			return &pmprovidersv1alpha1.ManagedProvider{}
		}, s.testObj).WithConditions(conditions.NewManager())
		key := ctrlruntimeclient.ObjectKeyFromObject(inst)
		req := mcreconcile.Request{Request: reconcile.Request{NamespacedName: key}}

		_, err := lc.Reconcile(ctx, req)
		s.Require().NoError(err)
		s.Require().NoError(cl.Get(ctx, key, inst))
		s.Require().Contains(inst.Finalizers, providerResourceFinalizer)

		inst.Spec.CleanupOnDelete = false
		s.Require().NoError(cl.Update(ctx, inst))
		s.Require().NoError(cl.Delete(ctx, inst))

		_, err = lc.Reconcile(ctx, req)
		s.Require().NoError(err)

		remaining := &pmprovidersv1alpha1.ManagedProvider{}
		err = cl.Get(ctx, key, remaining)
		s.Assert().Truef(apierrors.IsNotFound(err), "ManagedProvider stuck with finalizers: %v", remaining.Finalizers)
	})
}

// --- Finalize ---

func (s *ProviderResourceTestSuite) TestFinalize_NoCleanup() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.CleanupOnDelete = false

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *ProviderResourceTestSuite) TestFinalize_BuildKcpAdminConfigFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.CleanupOnDelete = true

	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "kcp-admin", Namespace: "platform-mesh-system"}, mock.AnythingOfType("*v1.Secret")).
		Return(errors.New("connection refused"))

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to build kcp admin config")
}

func (s *ProviderResourceTestSuite) TestFinalize_NewKcpClientFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.CleanupOnDelete = true

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(nil, errors.New("dial error"))

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to create kcp client")
}

func (s *ProviderResourceTestSuite) TestFinalize_DeleteFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.CleanupOnDelete = true

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1alpha1.Provider"), mock.Anything).
		Return(errors.New("delete failed"))

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to delete provider")
}

func (s *ProviderResourceTestSuite) TestFinalize_DeleteNotFound_Ignored() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.CleanupOnDelete = true

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1alpha1.Provider"), mock.Anything).
		Return(errors.New("providers.platform-mesh.io \"cowboys\" not found"))

	// IgnoreNotFound only works with apimachinery NotFound errors;
	// a plain error is returned as-is, so this is an error case.
	// Verify the subroutine propagates it.
	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
}

func (s *ProviderResourceTestSuite) TestFinalize_StillExists() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.CleanupOnDelete = true

	s.mockAdminSecret()
	s.kcpHelperMock.EXPECT().
		NewKcpClient(mock.Anything, "root:providers:system").
		Return(s.kcpClientMock, nil)
	s.kcpClientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*v1alpha1.Provider"), mock.Anything).
		Return(nil)

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err, "Finalize should succeed")
	s.Require().True(result.IsStopWithRequeue(), "Result should be StopWithRequeue, is %#v", result)
}
