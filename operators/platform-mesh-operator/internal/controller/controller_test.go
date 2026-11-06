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

package controller

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// fakeCtrlManager implements sigs.k8s.io/controller-runtime/pkg/manager.Manager for unit tests.
// Only GetClient and GetScheme are functional; all other methods panic if called.
type fakeCtrlManager struct {
	client ctrlruntimeclient.Client
	scheme *runtime.Scheme
}

func (f *fakeCtrlManager) GetClient() ctrlruntimeclient.Client             { return f.client }
func (f *fakeCtrlManager) GetScheme() *runtime.Scheme                      { return f.scheme }
func (f *fakeCtrlManager) GetConfig() *rest.Config                         { panic("not implemented") }
func (f *fakeCtrlManager) GetCache() cache.Cache                           { panic("not implemented") }
func (f *fakeCtrlManager) GetFieldIndexer() ctrlruntimeclient.FieldIndexer { panic("not implemented") }
func (f *fakeCtrlManager) GetEventRecorderFor(_ string) record.EventRecorder {
	panic("not implemented")
}
func (f *fakeCtrlManager) GetEventRecorder(_ string) events.EventRecorder { panic("not implemented") }
func (f *fakeCtrlManager) GetRESTMapper() meta.RESTMapper                 { panic("not implemented") }
func (f *fakeCtrlManager) GetAPIReader() ctrlruntimeclient.Reader         { panic("not implemented") }
func (f *fakeCtrlManager) GetHTTPClient() *http.Client                    { panic("not implemented") }
func (f *fakeCtrlManager) Add(_ ctrlmanager.Runnable) error               { return nil }
func (f *fakeCtrlManager) Elected() <-chan struct{}                       { panic("not implemented") }
func (f *fakeCtrlManager) AddMetricsServerExtraHandler(_ string, _ http.Handler) error {
	panic("not implemented")
}
func (f *fakeCtrlManager) AddHealthzCheck(_ string, _ healthz.Checker) error { return nil }
func (f *fakeCtrlManager) AddReadyzCheck(_ string, _ healthz.Checker) error  { return nil }
func (f *fakeCtrlManager) Start(_ context.Context) error                     { panic("not implemented") }
func (f *fakeCtrlManager) GetWebhookServer() webhook.Server                  { panic("not implemented") }
func (f *fakeCtrlManager) GetLogger() logr.Logger                            { return logr.Discard() }
func (f *fakeCtrlManager) GetControllerOptions() ctrlconfig.Controller {
	return ctrlconfig.Controller{}
}
func (f *fakeCtrlManager) GetConverterRegistry() conversion.Registry { panic("not implemented") }

// fakeManager wraps fakeCtrlManager and implements mcmanager.Manager for unit tests.
type fakeManager struct {
	*fakeCtrlManager
}

func newFakeManager(c ctrlruntimeclient.Client, s *runtime.Scheme) *fakeManager {
	return &fakeManager{fakeCtrlManager: &fakeCtrlManager{client: c, scheme: s}}
}

func (f *fakeManager) GetLocalManager() ctrlmanager.Manager { return f.fakeCtrlManager }
func (f *fakeManager) GetProvider() multicluster.Provider   { return nil }
func (f *fakeManager) GetCluster(_ context.Context, _ multicluster.ClusterName) (cluster.Cluster, error) {
	panic("not implemented")
}
func (f *fakeManager) ClusterFromContext(_ context.Context) (cluster.Cluster, error) {
	panic("not implemented")
}
func (f *fakeManager) GetManager(_ context.Context, _ multicluster.ClusterName) (ctrlmanager.Manager, error) {
	return f.fakeCtrlManager, nil
}
func (f *fakeManager) Engage(_ context.Context, _ multicluster.ClusterName, _ cluster.Cluster) error {
	return nil
}

// Satisfy mcmanager.Manager's Add method (takes mcmanager.Runnable, not ctrlmanager.Runnable)
func (f *fakeManager) Add(_ mcmanager.Runnable) error { return nil }

type MapConfigMapTestSuite struct {
	suite.Suite
	scheme *runtime.Scheme
}

func TestMapConfigMapTestSuite(t *testing.T) {
	suite.Run(t, new(MapConfigMapTestSuite))
}

func (s *MapConfigMapTestSuite) SetupSuite() {
	s.scheme = runtime.NewScheme()
	s.Require().NoError(clientgoscheme.AddToScheme(s.scheme))
	s.Require().NoError(pmcorev1alpha1.AddToScheme(s.scheme))
}

// newReconcilerWithClient builds a PlatformMeshReconciler whose client field
// is backed by the provided fake client (used by mapConfigMapToPlatformMesh).
func (s *MapConfigMapTestSuite) newReconcilerWithClient(c ctrlruntimeclient.Client) *PlatformMeshReconciler {
	return &PlatformMeshReconciler{client: c}
}

func (s *MapConfigMapTestSuite) Test_nonConfigMapObject_returnsEmpty() {
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	r := s.newReconcilerWithClient(fakeClient)

	// Pass a Secret (not a ConfigMap) — the type guard must return immediately.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "some-secret", Namespace: "default"},
	}
	reqs := r.mapConfigMapToPlatformMesh(context.Background(), secret)
	s.Empty(reqs)
}

func (s *MapConfigMapTestSuite) Test_listError_returnsEmpty() {
	// Build a fake client WITHOUT the corev1alpha1 scheme so List() fails.
	schemeWithoutPM := runtime.NewScheme()
	s.Require().NoError(clientgoscheme.AddToScheme(schemeWithoutPM))

	fakeClient := fake.NewClientBuilder().WithScheme(schemeWithoutPM).Build()
	r := s.newReconcilerWithClient(fakeClient)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "some-cm", Namespace: "default"},
	}
	reqs := r.mapConfigMapToPlatformMesh(context.Background(), cm)
	s.Empty(reqs)
}

func (s *MapConfigMapTestSuite) Test_noPlatformMeshes_returnsEmpty() {
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	r := s.newReconcilerWithClient(fakeClient)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pm-profile", Namespace: "default"},
	}
	reqs := r.mapConfigMapToPlatformMesh(context.Background(), cm)
	s.Empty(reqs)
}

func (s *MapConfigMapTestSuite) Test_configMapMatchesDefaultName_returnsRequest() {
	pm := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pm", Namespace: "default"},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(pm).
		Build()
	r := s.newReconcilerWithClient(fakeClient)

	// Default ConfigMap name is <pm-name>-profile in the same namespace.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pm-profile", Namespace: "default"},
	}
	reqs := r.mapConfigMapToPlatformMesh(context.Background(), cm)

	s.Require().Len(reqs, 1)
	s.Equal(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "my-pm", Namespace: "default"},
	}, reqs[0])
}

func (s *MapConfigMapTestSuite) Test_configMapMatchesExplicitRef_sameNamespace() {
	pm := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pm", Namespace: "ns-a"},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			ProfileConfigMap: &pmcorev1alpha1.ConfigMapReference{
				Name: "custom-profile",
				// No namespace set — should default to the PlatformMesh namespace.
			},
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(pm).
		Build()
	r := s.newReconcilerWithClient(fakeClient)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-profile", Namespace: "ns-a"},
	}
	reqs := r.mapConfigMapToPlatformMesh(context.Background(), cm)

	s.Require().Len(reqs, 1)
	s.Equal(types.NamespacedName{Name: "my-pm", Namespace: "ns-a"}, reqs[0].NamespacedName)
}

func (s *MapConfigMapTestSuite) Test_configMapMatchesExplicitRef_crossNamespace() {
	pm := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pm", Namespace: "ns-a"},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			ProfileConfigMap: &pmcorev1alpha1.ConfigMapReference{
				Name:      "shared-profile",
				Namespace: "shared-ns",
			},
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(pm).
		Build()
	r := s.newReconcilerWithClient(fakeClient)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "shared-profile", Namespace: "shared-ns"},
	}
	reqs := r.mapConfigMapToPlatformMesh(context.Background(), cm)

	s.Require().Len(reqs, 1)
	s.Equal(types.NamespacedName{Name: "my-pm", Namespace: "ns-a"}, reqs[0].NamespacedName)
}

func (s *MapConfigMapTestSuite) Test_configMapDoesNotMatchAny_returnsEmpty() {
	pm := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pm", Namespace: "default"},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			ProfileConfigMap: &pmcorev1alpha1.ConfigMapReference{
				Name: "expected-profile",
			},
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(pm).
		Build()
	r := s.newReconcilerWithClient(fakeClient)

	// A ConfigMap with a different name — should not match.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated-cm", Namespace: "default"},
	}
	reqs := r.mapConfigMapToPlatformMesh(context.Background(), cm)
	s.Empty(reqs)
}

func (s *MapConfigMapTestSuite) Test_multipleMatches_returnsAllMatchingRequests() {
	// pm1 uses default name "pm-one-profile" in "ns-one".
	pm1 := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "pm-one", Namespace: "ns-one"},
	}
	// pm2 explicitly references the same ConfigMap across namespaces.
	pm2 := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "pm-two", Namespace: "ns-two"},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			ProfileConfigMap: &pmcorev1alpha1.ConfigMapReference{
				Name:      "pm-one-profile",
				Namespace: "ns-one",
			},
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(pm1, pm2).
		Build()
	r := s.newReconcilerWithClient(fakeClient)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "pm-one-profile", Namespace: "ns-one"},
	}
	reqs := r.mapConfigMapToPlatformMesh(context.Background(), cm)

	s.Len(reqs, 2)
}

func (s *MapConfigMapTestSuite) Test_defaultNameWrongNamespace_doesNotMatch() {
	pm := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pm", Namespace: "ns-a"},
		// No explicit profileConfigMap — default is "my-pm-profile" in "ns-a".
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s.scheme).
		WithObjects(pm).
		Build()
	r := s.newReconcilerWithClient(fakeClient)

	// Correct name but wrong namespace.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pm-profile", Namespace: "ns-b"},
	}
	reqs := r.mapConfigMapToPlatformMesh(context.Background(), cm)
	s.Empty(reqs)
}

// ---- NewResourceReconciler nil clientInfra guard ----

type NewResourceReconcilerTestSuite struct {
	suite.Suite
	scheme *runtime.Scheme
}

func TestNewResourceReconcilerTestSuite(t *testing.T) {
	suite.Run(t, new(NewResourceReconcilerTestSuite))
}

func (s *NewResourceReconcilerTestSuite) SetupSuite() {
	s.scheme = runtime.NewScheme()
	s.Require().NoError(clientgoscheme.AddToScheme(s.scheme))
	s.Require().NoError(pmcorev1alpha1.AddToScheme(s.scheme))
}

func (s *NewResourceReconcilerTestSuite) Test_nilClientInfra_usesManagerClient() {
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	mgr := newFakeManager(fakeClient, s.scheme)
	cfg := &config.OperatorConfig{}

	// Must not panic and must return a non-nil reconciler.
	r, err := NewResourceReconciler(mgr, cfg, nil, nil)
	s.Require().NoError(err)
	s.NotNil(r)
	s.NotNil(r.lifecycle)
}

func (s *NewResourceReconcilerTestSuite) Test_withClientInfra_usesProvidedClient() {
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	infraClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	mgr := newFakeManager(fakeClient, s.scheme)
	cfg := &config.OperatorConfig{}

	r, err := NewResourceReconciler(mgr, cfg, infraClient, nil)
	s.Require().NoError(err)
	s.NotNil(r)
	s.NotNil(r.lifecycle)
}

// ---- NewPlatformMeshReconciler subroutine selection ----

type NewPlatformMeshReconcilerTestSuite struct {
	suite.Suite
	scheme *runtime.Scheme
}

func TestNewPlatformMeshReconcilerTestSuite(t *testing.T) {
	suite.Run(t, new(NewPlatformMeshReconcilerTestSuite))
}

func (s *NewPlatformMeshReconcilerTestSuite) SetupSuite() {
	s.scheme = runtime.NewScheme()
	s.Require().NoError(clientgoscheme.AddToScheme(s.scheme))
	s.Require().NoError(pmcorev1alpha1.AddToScheme(s.scheme))
}

func (s *NewPlatformMeshReconcilerTestSuite) Test_allSubroutinesDisabled_returnsValidReconciler() {
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	mgr := newFakeManager(fakeClient, s.scheme)
	cfg := &config.OperatorConfig{
		Subroutines: config.SubroutinesConfig{
			Deployment:     config.DeploymentSubroutineConfig{Enabled: false},
			KcpSetup:       config.KcpSetupSubroutineConfig{Enabled: false},
			ProviderSecret: config.ProviderSecretSubroutineConfig{Enabled: false},
			FeatureToggles: config.FeatureTogglesSubroutineConfig{Enabled: false},
			Wait:           config.WaitSubroutineConfig{Enabled: false},
		},
	}
	commonCfg := &pmconfig.CommonServiceConfig{}

	r, err := NewPlatformMeshReconciler(mgr, cfg, commonCfg, fakeClient, subroutines.NewImageVersionStore())
	s.Require().NoError(err)
	s.NotNil(r)
	s.NotNil(r.lifecycle)
	s.Equal(fakeClient, r.client)
}

func (s *NewPlatformMeshReconcilerTestSuite) Test_deploymentSubroutineEnabled_returnsValidReconciler() {
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	mgr := newFakeManager(fakeClient, s.scheme)
	cfg := &config.OperatorConfig{
		Subroutines: config.SubroutinesConfig{
			Deployment: config.DeploymentSubroutineConfig{Enabled: true},
		},
	}
	commonCfg := &pmconfig.CommonServiceConfig{}

	r, err := NewPlatformMeshReconciler(mgr, cfg, commonCfg, fakeClient, subroutines.NewImageVersionStore())
	s.Require().NoError(err)
	s.NotNil(r)
	s.NotNil(r.lifecycle)
}

func (s *NewPlatformMeshReconcilerTestSuite) Test_kcpSetupSubroutineEnabled_returnsValidReconciler() {
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	mgr := newFakeManager(fakeClient, s.scheme)
	cfg := &config.OperatorConfig{
		Subroutines: config.SubroutinesConfig{
			KcpSetup: config.KcpSetupSubroutineConfig{Enabled: true},
		},
	}
	commonCfg := &pmconfig.CommonServiceConfig{}

	r, err := NewPlatformMeshReconciler(mgr, cfg, commonCfg, fakeClient, nil)
	s.Require().NoError(err)
	s.NotNil(r)
	s.NotNil(r.lifecycle)
}

func (s *NewPlatformMeshReconcilerTestSuite) Test_waitSubroutineEnabled_returnsValidReconciler() {
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	mgr := newFakeManager(fakeClient, s.scheme)
	cfg := &config.OperatorConfig{
		Subroutines: config.SubroutinesConfig{
			Wait: config.WaitSubroutineConfig{Enabled: true},
		},
	}
	commonCfg := &pmconfig.CommonServiceConfig{}

	r, err := NewPlatformMeshReconciler(mgr, cfg, commonCfg, fakeClient, nil)
	s.Require().NoError(err)
	s.NotNil(r)
	s.NotNil(r.lifecycle)
}

func (s *NewPlatformMeshReconcilerTestSuite) Test_providerSecretSubroutineEnabled_returnsValidReconciler() {
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	mgr := newFakeManager(fakeClient, s.scheme)
	cfg := &config.OperatorConfig{
		Subroutines: config.SubroutinesConfig{
			ProviderSecret: config.ProviderSecretSubroutineConfig{Enabled: true},
		},
	}
	commonCfg := &pmconfig.CommonServiceConfig{}

	r, err := NewPlatformMeshReconciler(mgr, cfg, commonCfg, fakeClient, nil)
	s.Require().NoError(err)
	s.NotNil(r)
	s.NotNil(r.lifecycle)
}

func (s *NewPlatformMeshReconcilerTestSuite) Test_featureTogglesSubroutineEnabled_returnsValidReconciler() {
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	mgr := newFakeManager(fakeClient, s.scheme)
	cfg := &config.OperatorConfig{
		Subroutines: config.SubroutinesConfig{
			FeatureToggles: config.FeatureTogglesSubroutineConfig{Enabled: true},
		},
	}
	commonCfg := &pmconfig.CommonServiceConfig{}

	r, err := NewPlatformMeshReconciler(mgr, cfg, commonCfg, fakeClient, nil)
	s.Require().NoError(err)
	s.NotNil(r)
	s.NotNil(r.lifecycle)
}
