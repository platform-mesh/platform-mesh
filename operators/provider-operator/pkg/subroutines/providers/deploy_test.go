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
	"fmt"
	"testing"

	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/provider-operator/pkg/subroutines/mocks"
)

type DeployTestSuite struct {
	suite.Suite
	testObj    *DeploySubroutine
	clientMock *mocks.Client
	log        *logger.Logger
}

func TestDeployTestSuite(t *testing.T) {
	suite.Run(t, new(DeployTestSuite))
}

func (s *DeployTestSuite) SetupTest() {
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "DeployTestSuite"
	s.log, _ = logger.New(cfg)

	s.clientMock = new(mocks.Client)
	s.clientMock.EXPECT().Scheme().Return(runtime.NewScheme()).Maybe()

	var err error
	s.testObj, err = NewDeploySubroutine(s.clientMock)
	s.Require().NoError(err)
}

func (s *DeployTestSuite) TearDownTest() {
	s.clientMock = nil
	s.testObj = nil
}

func (s *DeployTestSuite) newCtx() context.Context {
	return context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)
}

// newManagedProvider returns a ManagedProvider with a single RuntimeDeployment
// (github.com/platform-mesh/wildwest-controller → resource name "wildwest-controller").
func (s *DeployTestSuite) newManagedProvider() *providersv1alpha1.ManagedProvider {
	return &providersv1alpha1.ManagedProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cowboys",
			Namespace: "providers-wildwest-ns",
		},
		Spec: providersv1alpha1.ManagedProviderSpec{
			RuntimeDeployments: []providersv1alpha1.ProviderComponentSpec{
				{Flux: &providersv1alpha1.FluxComponentSpec{
					Chart:    "github.com/platform-mesh/wildwest-controller",
					Version:  "0.1.0",
					Registry: "ghcr.io/platform-mesh/ocm",
				}},
			},
		},
	}
}

// newManagedProviderWithPortal returns a ManagedProvider with two RuntimeDeployments.
func (s *DeployTestSuite) newManagedProviderWithPortal() *providersv1alpha1.ManagedProvider {
	inst := s.newManagedProvider()
	inst.Spec.RuntimeDeployments = append(inst.Spec.RuntimeDeployments, providersv1alpha1.ProviderComponentSpec{
		Flux: &providersv1alpha1.FluxComponentSpec{
			Chart:    "github.com/platform-mesh/wildwest-portal",
			Version:  "0.1.0",
			Registry: "ghcr.io/platform-mesh/ocm",
		},
	})
	return inst
}

func (s *DeployTestSuite) mockCreateOrUpdate(name, namespace string) {
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: name, Namespace: namespace}, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(kerrors.NewNotFound(schema.GroupResource{}, name)).
		Once()
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(nil).
		Once()
}

func (s *DeployTestSuite) mockHelmReleaseReadyCheck(name, namespace string, ready bool) {
	var conditions []interface{}
	if ready {
		conditions = []interface{}{
			map[string]interface{}{"type": "Ready", "status": "True"},
		}
	}
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: name, Namespace: namespace}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
			u := obj.(*unstructured.Unstructured)
			u.Object = map[string]interface{}{
				"status": map[string]interface{}{"conditions": conditions},
			}
			return nil
		}).
		Once()
}

// mockExistingNoopOCIRepo simulates an OCIRepository that already exists with a
// matching spec (OperationResultNone) and whose generation matches observedGeneration,
// indicating the artifact controller has fully processed the current spec.
func (s *DeployTestSuite) mockExistingNoopOCIRepo(name, namespace string, flux *providersv1alpha1.FluxComponentSpec) {
	ociURL := fmt.Sprintf("oci://%s/%s", flux.Registry, flux.Chart)
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: name, Namespace: namespace}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			u := obj.(*unstructured.Unstructured)
			u.SetResourceVersion("1")
			u.SetGeneration(1)
			_ = unstructured.SetNestedField(u.Object, ociURL, "spec", "url")
			_ = unstructured.SetNestedField(u.Object, flux.Version, "spec", "ref", "tag")
			_ = unstructured.SetNestedField(u.Object, "generic", "spec", "provider")
			_ = unstructured.SetNestedField(u.Object, "1m0s", "spec", "interval")
			_ = unstructured.SetNestedField(u.Object, flux.Insecure, "spec", "insecure")
			_ = unstructured.SetNestedMap(u.Object, map[string]interface{}{
				"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
				"operation": "copy",
			}, "spec", "layerSelector")
			_ = unstructured.SetNestedField(u.Object, int64(1), "status", "observedGeneration")
			return nil
		}).Once()
}

// mockExistingNoopHelmRelease simulates a HelmRelease that already exists with a
// matching spec (OperationResultNone).
func (s *DeployTestSuite) mockExistingNoopHelmRelease(name, namespace string) {
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: name, Namespace: namespace}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			u := obj.(*unstructured.Unstructured)
			u.SetResourceVersion("1")
			_ = unstructured.SetNestedField(u.Object, "5m", "spec", "interval")
			_ = unstructured.SetNestedField(u.Object, "OCIRepository", "spec", "chartRef", "kind")
			_ = unstructured.SetNestedField(u.Object, name, "spec", "chartRef", "name")
			_ = unstructured.SetNestedField(u.Object, namespace, "spec", "chartRef", "namespace")
			return nil
		}).Once()
}

// mockComponentDeployed sets up mocks for a component already present on the cluster
// with a matching spec (OperationResultNone, generation reconciled) and the given
// HelmRelease readiness state. Use this to test the condition-check path.
func (s *DeployTestSuite) mockComponentDeployed(name, namespace string, flux *providersv1alpha1.FluxComponentSpec, ready bool) {
	s.mockExistingNoopOCIRepo(name, namespace, flux)
	s.mockExistingNoopHelmRelease(name, namespace)
	s.mockHelmReleaseReadyCheck(name, namespace, ready)
}

// --- Process tests ---

func (s *DeployTestSuite) TestProcess_OCIRepositoryCreateFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	// Component name "github.com/platform-mesh/wildwest-controller" → "wildwest-controller"
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(kerrors.NewNotFound(schema.GroupResource{}, "wildwest-controller")).
		Once()
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(errors.New("server error")).
		Once()

	result, err := s.testObj.Process(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to reconcile OCIRepository")
}

func (s *DeployTestSuite) TestProcess_HelmReleaseCreateFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.mockCreateOrUpdate("wildwest-controller", "providers-wildwest-ns") // OCIRepository OK
	// HelmRelease Get → NotFound, Create → error
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(kerrors.NewNotFound(schema.GroupResource{}, "wildwest-controller")).
		Once()
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(errors.New("helm release create failed")).
		Once()

	result, err := s.testObj.Process(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to reconcile HelmRelease")
}

func (s *DeployTestSuite) TestProcess_HelmReleaseGetFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	flux := inst.Spec.RuntimeDeployments[0].Flux

	s.mockExistingNoopOCIRepo("wildwest-controller", "providers-wildwest-ns", flux)
	s.mockExistingNoopHelmRelease("wildwest-controller", "providers-wildwest-ns")
	// helmReleaseReady Get → non-404 error
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(errors.New("internal server error")).
		Once()

	result, err := s.testObj.Process(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to get HelmRelease")
}

func (s *DeployTestSuite) TestProcess_ControllerNotReady() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	flux := inst.Spec.RuntimeDeployments[0].Flux

	s.mockComponentDeployed("wildwest-controller", "providers-wildwest-ns", flux, false)

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseDeploying, inst.Status.Phase)
}

func (s *DeployTestSuite) TestProcess_ControllerReady_NoPortal() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	flux := inst.Spec.RuntimeDeployments[0].Flux

	s.mockComponentDeployed("wildwest-controller", "providers-wildwest-ns", flux, true)

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseReady, inst.Status.Phase)
}

func (s *DeployTestSuite) TestProcess_ControllerReady_PortalNotReady() {
	ctx := s.newCtx()
	inst := s.newManagedProviderWithPortal()
	controllerFlux := inst.Spec.RuntimeDeployments[0].Flux
	portalFlux := inst.Spec.RuntimeDeployments[1].Flux

	s.mockComponentDeployed("wildwest-controller", "providers-wildwest-ns", controllerFlux, true)
	s.mockComponentDeployed("wildwest-portal", "providers-wildwest-ns", portalFlux, false)

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseDeploying, inst.Status.Phase)
}

func (s *DeployTestSuite) TestProcess_ControllerReady_PortalReady() {
	ctx := s.newCtx()
	inst := s.newManagedProviderWithPortal()
	controllerFlux := inst.Spec.RuntimeDeployments[0].Flux
	portalFlux := inst.Spec.RuntimeDeployments[1].Flux

	s.mockComponentDeployed("wildwest-controller", "providers-wildwest-ns", controllerFlux, true)
	s.mockComponentDeployed("wildwest-portal", "providers-wildwest-ns", portalFlux, true)

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseReady, inst.Status.Phase)
}

func (s *DeployTestSuite) TestProcess_HelmReleaseNotFoundDuringReadyCheck() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	flux := inst.Spec.RuntimeDeployments[0].Flux

	s.mockExistingNoopOCIRepo("wildwest-controller", "providers-wildwest-ns", flux)
	s.mockExistingNoopHelmRelease("wildwest-controller", "providers-wildwest-ns")
	// helmReleaseReady: NotFound → treated as not ready, no error
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(kerrors.NewNotFound(schema.GroupResource{}, "wildwest-controller")).
		Once()

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseDeploying, inst.Status.Phase)
}

func (s *DeployTestSuite) TestProcess_WithHelmValues() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	inst.Spec.RuntimeDeployments[0].Flux.Values = apiextensionsv1.JSON{
		Raw: []byte(`{"replicaCount":2,"image":{"tag":"v0.1.0"}}`),
	}

	// Capture the HelmRelease Create call to verify values are injected.
	var capturedHR *unstructured.Unstructured
	s.mockCreateOrUpdate("wildwest-controller", "providers-wildwest-ns") // OCIRepository
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(kerrors.NewNotFound(schema.GroupResource{}, "wildwest-controller")).
		Once()
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		RunAndReturn(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
			capturedHR = obj.(*unstructured.Unstructured).DeepCopy()
			return nil
		}).
		Once()
	result, err := s.testObj.Process(ctx, inst)

	// Both resources were Created → immediate requeue without checking HelmRelease conditions.
	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseDeploying, inst.Status.Phase)
	s.Require().NotNil(capturedHR)
	vals, found, _ := unstructured.NestedMap(capturedHR.Object, "spec", "values")
	s.Assert().True(found)
	s.Assert().EqualValues(float64(2), vals["replicaCount"])
}

func (s *DeployTestSuite) TestProcess_ExistingResourcesUpdated() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	// OCIRepository already exists → Update
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
			u := obj.(*unstructured.Unstructured)
			u.SetResourceVersion("1")
			return nil
		}).
		Once()
	s.clientMock.EXPECT().
		Update(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(nil).
		Once()
	// HelmRelease already exists → Update
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
			u := obj.(*unstructured.Unstructured)
			u.SetResourceVersion("2")
			return nil
		}).
		Once()
	s.clientMock.EXPECT().
		Update(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(nil).
		Once()

	result, err := s.testObj.Process(ctx, inst)

	// Both resources were Updated → immediate requeue, HelmRelease conditions not checked.
	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseDeploying, inst.Status.Phase)
}

func (s *DeployTestSuite) TestProcess_ResourcesCreated_ImmediateRequeue() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	// Fresh Create for both resources → OperationResultCreated → immediate StopWithRequeue.
	s.mockCreateOrUpdate("wildwest-controller", "providers-wildwest-ns") // OCIRepository
	s.mockCreateOrUpdate("wildwest-controller", "providers-wildwest-ns") // HelmRelease

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseDeploying, inst.Status.Phase)
}

func (s *DeployTestSuite) TestProcess_GenerationMismatch_Requeue() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()
	flux := inst.Spec.RuntimeDeployments[0].Flux
	ociURL := fmt.Sprintf("oci://%s/%s", flux.Registry, flux.Chart)

	// OCIRepository exists but observedGeneration lags behind generation → Flux hasn't
	// fully processed the current spec yet.
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			u := obj.(*unstructured.Unstructured)
			u.SetResourceVersion("1")
			u.SetGeneration(2)
			_ = unstructured.SetNestedField(u.Object, ociURL, "spec", "url")
			_ = unstructured.SetNestedField(u.Object, flux.Version, "spec", "ref", "tag")
			_ = unstructured.SetNestedField(u.Object, "generic", "spec", "provider")
			_ = unstructured.SetNestedField(u.Object, "1m0s", "spec", "interval")
			_ = unstructured.SetNestedField(u.Object, flux.Insecure, "spec", "insecure")
			_ = unstructured.SetNestedMap(u.Object, map[string]interface{}{
				"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
				"operation": "copy",
			}, "spec", "layerSelector")
			_ = unstructured.SetNestedField(u.Object, int64(1), "status", "observedGeneration") // lag
			return nil
		}).Once()
	s.mockExistingNoopHelmRelease("wildwest-controller", "providers-wildwest-ns")

	result, err := s.testObj.Process(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseDeploying, inst.Status.Phase)
}

// --- Finalize tests ---

func (s *DeployTestSuite) TestFinalize_ControllerOnly_Requeues() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	// HelmRelease and OCIRepository delete both succeed → still pending deletion → requeue
	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(nil).
		Times(2)

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.clientMock.AssertExpectations(s.T())
}

func (s *DeployTestSuite) TestFinalize_WithPortal_Requeues() {
	ctx := s.newCtx()
	inst := s.newManagedProviderWithPortal()

	// wildwest-controller HelmRelease, wildwest-controller OCIRepository,
	// wildwest-portal HelmRelease, wildwest-portal OCIRepository
	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(nil).
		Times(4)

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.clientMock.AssertExpectations(s.T())
}

func (s *DeployTestSuite) TestFinalize_AllAlreadyGone() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	// Both resources already gone → allGone = true → OK
	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(kerrors.NewNotFound(schema.GroupResource{}, "wildwest-controller")).
		Times(2)

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsContinue())
}

func (s *DeployTestSuite) TestFinalize_HelmReleaseDeleteFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(errors.New("delete failed")).
		Once()

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to delete HelmRelease")
}

func (s *DeployTestSuite) TestFinalize_OCIRepoDeleteFails() {
	ctx := s.newCtx()
	inst := s.newManagedProvider()

	// HelmRelease delete OK, OCIRepository delete fails
	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(nil).
		Once()
	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		Return(errors.New("delete failed")).
		Once()

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().Error(err)
	s.Assert().True(result.IsContinue())
	s.Assert().Contains(err.Error(), "failed to delete OCIRepository")
}

// --- type=helm (classic HTTP Helm repository) tests ---

// newManagedProviderHelmRepo returns a ManagedProvider with a single type=helm
// RuntimeDeployment sourced from a classic HTTP(S) Helm repository.
func (s *DeployTestSuite) newManagedProviderHelmRepo() *providersv1alpha1.ManagedProvider {
	inst := s.newManagedProvider()
	inst.Spec.RuntimeDeployments = []providersv1alpha1.ProviderComponentSpec{
		{Flux: &providersv1alpha1.FluxComponentSpec{
			Type:     providersv1alpha1.FluxSourceTypeHelm,
			Registry: "https://charts.jetstack.io",
			Chart:    "cert-manager",
			Version:  "v1.14.0",
		}},
	}
	return inst
}

func (s *DeployTestSuite) TestProcess_HelmRepo_CreatesHelmRepositoryAndRelease() {
	ctx := s.newCtx()
	inst := s.newManagedProviderHelmRepo()

	var created []*unstructured.Unstructured
	// Both HelmRepository and HelmRelease are absent → Get NotFound, then Create.
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "cert-manager", Namespace: "providers-wildwest-ns"}, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(kerrors.NewNotFound(schema.GroupResource{}, "cert-manager")).
		Times(2)
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		RunAndReturn(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
			created = append(created, obj.(*unstructured.Unstructured).DeepCopy())
			return nil
		}).
		Times(2)

	result, err := s.testObj.Process(ctx, inst)

	// Both resources Created → immediate requeue without checking conditions.
	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseDeploying, inst.Status.Phase)
	s.Require().Len(created, 2)

	// First object is the HelmRepository pointing at the HTTP repo URL.
	repo := created[0]
	s.Assert().Equal("HelmRepository", repo.GetKind())
	url, _, _ := unstructured.NestedString(repo.Object, "spec", "url")
	s.Assert().Equal("https://charts.jetstack.io", url)

	// Second object is the HelmRelease wired via chart.spec.sourceRef → HelmRepository
	// (not chartRef, which is the OCI path).
	hr := created[1]
	s.Assert().Equal("HelmRelease", hr.GetKind())
	chart, _, _ := unstructured.NestedString(hr.Object, "spec", "chart", "spec", "chart")
	s.Assert().Equal("cert-manager", chart)
	version, _, _ := unstructured.NestedString(hr.Object, "spec", "chart", "spec", "version")
	s.Assert().Equal("v1.14.0", version)
	srcKind, _, _ := unstructured.NestedString(hr.Object, "spec", "chart", "spec", "sourceRef", "kind")
	s.Assert().Equal("HelmRepository", srcKind)
	_, found, _ := unstructured.NestedMap(hr.Object, "spec", "chartRef")
	s.Assert().False(found, "type=helm must not set chartRef")
}

func (s *DeployTestSuite) TestFinalize_HelmRepo_DeletesHelmRepository() {
	ctx := s.newCtx()
	inst := s.newManagedProviderHelmRepo()

	var deletedKinds []string
	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		RunAndReturn(func(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
			deletedKinds = append(deletedKinds, obj.(*unstructured.Unstructured).GetKind())
			return nil
		}).
		Times(2)

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Contains(deletedKinds, "HelmRelease")
	s.Assert().Contains(deletedKinds, "HelmRepository")
}

// --- ocm (OCM descriptor resolution) tests ---

func (s *DeployTestSuite) TestOCMResolvedOCIURL() {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cases := []struct{ name, imageRef, version, want string }{
		{"tag form", "oci://ghcr.io/platform-mesh/charts/wildwest:1.2.3", "1.2.3", "oci://ghcr.io/platform-mesh/charts/wildwest"},
		{"no scheme", "ghcr.io/platform-mesh/charts/wildwest:1.2.3", "1.2.3", "oci://ghcr.io/platform-mesh/charts/wildwest"},
		{"digest form", "oci://ghcr.io/platform-mesh/charts/wildwest@" + digest, "1.2.3", "oci://ghcr.io/platform-mesh/charts/wildwest"},
	}
	for _, tc := range cases {
		got, err := ocmResolvedOCIURL(tc.imageRef, tc.version)
		s.Require().NoError(err, tc.name)
		s.Assert().Equal(tc.want, got, tc.name)
	}
}

func (s *DeployTestSuite) TestOCMDeploymentName() {
	s.Equal("explicit", ocmDeploymentName(&providersv1alpha1.OCMComponentSpec{
		Name: "explicit", Component: "github.com/x/comp",
	}))
	s.Equal("controller-chart", ocmDeploymentName(&providersv1alpha1.OCMComponentSpec{
		ResourceName: "controller-chart", Component: "github.com/x/comp",
	}))
	s.Equal("last-ref", ocmDeploymentName(&providersv1alpha1.OCMComponentSpec{
		ReferencePath: []providersv1alpha1.OCMReferencePathElement{{Name: "first"}, {Name: "last-ref"}},
		Component:     "github.com/x/comp",
	}))
	s.Equal("comp", ocmDeploymentName(&providersv1alpha1.OCMComponentSpec{
		Component: "github.com/platform-mesh/comp",
	}))
}

func (s *DeployTestSuite) TestSplitRegistry() {
	base, sub := splitRegistry("ghcr.io/platform-mesh")
	s.Equal("ghcr.io", base)
	s.Equal("platform-mesh", sub)
	base, sub = splitRegistry("ghcr.io/platform-mesh/provider-quickstart/charts")
	s.Equal("ghcr.io", base)
	s.Equal("platform-mesh/provider-quickstart/charts", sub)
	base, sub = splitRegistry("ghcr.io")
	s.Equal("ghcr.io", base)
	s.Equal("", sub)
}

// newManagedProviderOCM returns a ManagedProvider with a single self-contained ocm
// RuntimeDeployment; the deployment name is explicit ("wildwest-controller") and the
// operator generates Repository + Component + Resource from the inline coordinates.
func (s *DeployTestSuite) newManagedProviderOCM() *providersv1alpha1.ManagedProvider {
	inst := s.newManagedProvider()
	inst.Spec.RuntimeDeployments = []providersv1alpha1.ProviderComponentSpec{
		{OCM: &providersv1alpha1.OCMComponentSpec{
			Name:         "wildwest-controller",
			Registry:     "ghcr.io/platform-mesh",
			Component:    "github.com/platform-mesh/provider-quickstart",
			Version:      "0.0.8",
			ResourceName: "controller-chart",
		}},
	}
	return inst
}

func (s *DeployTestSuite) TestProcess_OCM_CreatesOCMObjects() {
	ctx := s.newCtx()
	inst := s.newManagedProviderOCM()
	const ns = "providers-wildwest-ns"

	// Repository, Component and Resource all absent → Get NotFound then Create.
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: ns}, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(kerrors.NewNotFound(schema.GroupResource{}, "wildwest-controller")).
		Times(3)
	created := map[string]*unstructured.Unstructured{}
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		RunAndReturn(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
			u := obj.(*unstructured.Unstructured).DeepCopy()
			created[u.GetKind()] = u
			return nil
		}).
		Times(3)

	result, err := s.testObj.Process(ctx, inst)

	// OCM objects just created → requeue for resolution; no Flux objects yet.
	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseDeploying, inst.Status.Phase)

	repo := created["Repository"]
	s.Require().NotNil(repo, "operator must create the Repository")
	baseURL, _, _ := unstructured.NestedString(repo.Object, "spec", "repositorySpec", "baseUrl")
	subPath, _, _ := unstructured.NestedString(repo.Object, "spec", "repositorySpec", "subPath")
	s.Assert().Equal("ghcr.io", baseURL)
	s.Assert().Equal("platform-mesh", subPath)

	comp := created["Component"]
	s.Require().NotNil(comp, "operator must create the Component")
	cname, _, _ := unstructured.NestedString(comp.Object, "spec", "component")
	semver, _, _ := unstructured.NestedString(comp.Object, "spec", "semver")
	repoRef, _, _ := unstructured.NestedString(comp.Object, "spec", "repositoryRef", "name")
	s.Assert().Equal("github.com/platform-mesh/provider-quickstart", cname)
	s.Assert().Equal("0.0.8", semver)
	s.Assert().Equal("wildwest-controller", repoRef)

	res := created["Resource"]
	s.Require().NotNil(res, "operator must create the Resource")
	s.Assert().Equal("chart", res.GetLabels()["artifact"])
	s.Assert().Equal("oci", res.GetLabels()["repo"])
	compRef, _, _ := unstructured.NestedString(res.Object, "spec", "componentRef", "name")
	s.Assert().Equal("wildwest-controller", compRef)
	resName, _, _ := unstructured.NestedString(res.Object, "spec", "resource", "byReference", "resource", "name")
	s.Assert().Equal("controller-chart", resName)
	ocmConfig, _, _ := unstructured.NestedSlice(res.Object, "spec", "ocmConfig")
	s.Require().Len(ocmConfig, 1)
	s.Assert().Equal("wildwest-controller", ocmConfig[0].(map[string]interface{})["name"])
}

func (s *DeployTestSuite) TestProcess_OCM_Resolved_DeploysChart() {
	ctx := s.newCtx()
	inst := s.newManagedProviderOCM()
	const ns = "providers-wildwest-ns"

	repoRef := []interface{}{map[string]interface{}{
		"apiVersion": "delivery.ocm.software/v1alpha1", "kind": "Repository", "name": "wildwest-controller", "namespace": ns, "policy": "Propagate",
	}}

	// Repository, Component, Resource already exist with the desired spec
	// (CreateOrUpdate → None); the Resource also carries a resolved status.
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: ns}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			u := obj.(*unstructured.Unstructured)
			u.SetResourceVersion("1")
			_ = unstructured.SetNestedField(u.Object, "1m0s", "spec", "interval")
			_ = unstructured.SetNestedMap(u.Object, map[string]interface{}{"type": "OCIRegistry", "baseUrl": "ghcr.io", "subPath": "platform-mesh"}, "spec", "repositorySpec")
			return nil
		}).
		Once()
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: ns}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			u := obj.(*unstructured.Unstructured)
			u.SetResourceVersion("1")
			_ = unstructured.SetNestedField(u.Object, "github.com/platform-mesh/provider-quickstart", "spec", "component")
			_ = unstructured.SetNestedField(u.Object, "0.0.8", "spec", "semver")
			_ = unstructured.SetNestedField(u.Object, "1m0s", "spec", "interval")
			_ = unstructured.SetNestedField(u.Object, "Deny", "spec", "downgradePolicy")
			_ = unstructured.SetNestedField(u.Object, "wildwest-controller", "spec", "repositoryRef", "name")
			_ = unstructured.SetNestedSlice(u.Object, repoRef, "spec", "ocmConfig")
			return nil
		}).
		Once()
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: ns}, mock.AnythingOfType("*unstructured.Unstructured")).
		RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
			u := obj.(*unstructured.Unstructured)
			u.SetResourceVersion("1")
			u.SetLabels(map[string]string{"artifact": "chart", "repo": "oci"})
			_ = unstructured.SetNestedField(u.Object, "wildwest-controller", "spec", "componentRef", "name")
			_ = unstructured.SetNestedField(u.Object, "controller-chart", "spec", "resource", "byReference", "resource", "name")
			_ = unstructured.SetNestedSlice(u.Object, repoRef, "spec", "ocmConfig")
			_ = unstructured.SetNestedField(u.Object, "oci://ghcr.io/platform-mesh/provider-quickstart/controller-chart:0.0.8", "status", "resource", "access", "imageReference")
			_ = unstructured.SetNestedField(u.Object, "0.0.8", "status", "resource", "version")
			return nil
		}).
		Once()
	// OCIRepository + HelmRelease both absent → Get NotFound then Create.
	s.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{Name: "wildwest-controller", Namespace: ns}, mock.AnythingOfType("*unstructured.Unstructured")).
		Return(kerrors.NewNotFound(schema.GroupResource{}, "wildwest-controller")).
		Times(2)
	var createdKinds []string
	var ociRepo *unstructured.Unstructured
	s.clientMock.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		RunAndReturn(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
			u := obj.(*unstructured.Unstructured).DeepCopy()
			createdKinds = append(createdKinds, u.GetKind())
			if u.GetKind() == "OCIRepository" {
				ociRepo = u
			}
			return nil
		}).
		Times(2)

	result, err := s.testObj.Process(ctx, inst)

	// Both Flux objects Created → immediate requeue.
	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Equal(providersv1alpha1.ManagedProviderPhaseDeploying, inst.Status.Phase)
	s.Assert().Contains(createdKinds, "OCIRepository")
	s.Assert().Contains(createdKinds, "HelmRelease")
	s.Require().NotNil(ociRepo, "OCIRepository should have been created from the resolved status")
	url, _, _ := unstructured.NestedString(ociRepo.Object, "spec", "url")
	s.Assert().Equal("oci://ghcr.io/platform-mesh/provider-quickstart/controller-chart", url)
	tag, _, _ := unstructured.NestedString(ociRepo.Object, "spec", "ref", "tag")
	s.Assert().Equal("0.0.8", tag)
}

func (s *DeployTestSuite) TestFinalize_OCM_DeletesOCMAndFluxObjects() {
	ctx := s.newCtx()
	inst := s.newManagedProviderOCM()

	var deletedKinds []string
	s.clientMock.EXPECT().
		Delete(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
		RunAndReturn(func(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
			deletedKinds = append(deletedKinds, obj.(*unstructured.Unstructured).GetKind())
			return nil
		}).
		Times(5)

	result, err := s.testObj.Finalize(ctx, inst)

	s.Require().NoError(err)
	s.Assert().True(result.IsStopWithRequeue())
	s.Assert().Contains(deletedKinds, "HelmRelease")
	s.Assert().Contains(deletedKinds, "OCIRepository")
	s.Assert().Contains(deletedKinds, "Resource")
	s.Assert().Contains(deletedKinds, "Component")
	s.Assert().Contains(deletedKinds, "Repository")
}
