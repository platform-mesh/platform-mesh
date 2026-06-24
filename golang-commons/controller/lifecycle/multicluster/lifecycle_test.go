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

package multicluster

import (
	"context"
	goerrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	pmtesting "go.platform-mesh.io/golang-commons/controller/testSupport"
	operrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger/testlogger"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	controllerruntime "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

func TestLifecycle(t *testing.T) {
	namespace := "bar"
	name := "foo"
	testApiObject := &pmtesting.TestApiObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	t.Run("Should setup with manager ok", func(t *testing.T) {
		// Arrange
		instance := &corev1.Namespace{}
		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr, log := createLifecycleManager([]subroutine.Subroutine{}, fakeClient)

		tr := &testReconciler{
			lifecycleManager: mgr,
		}

		// Act
		cfg := &rest.Config{}
		provider := pmtesting.NewFakeProvider(cfg)
		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		mmanager, err := mcmanager.New(cfg, provider, mcmanager.Options{Scheme: scheme})
		assert.NoError(t, err)
		err = mgr.SetupWithManager(mmanager, 0, "testReconciler", instance, "test", tr, log.Logger)

		// Assert
		assert.NoError(t, err)
	})
	t.Run("Should setup with manager not implementing interface", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.NotImplementingSpreadReconciles{}
		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr, log := createLifecycleManager([]subroutine.Subroutine{}, fakeClient)
		mgr.WithSpreadingReconciles()
		tr := &testReconciler{
			lifecycleManager: mgr,
		}

		// Act
		cfg := &rest.Config{}
		provider := pmtesting.NewFakeProvider(cfg)
		mmanager, err := mcmanager.New(cfg, provider, mcmanager.Options{})
		assert.NoError(t, err)
		err = mgr.SetupWithManager(mmanager, 0, "testReconciler", instance, "test", tr, log.Logger)

		// Assert
		assert.Error(t, err)
	})
	t.Run("Should setup with manager read only", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.NotImplementingSpreadReconciles{}
		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr, log := createLifecycleManager([]subroutine.Subroutine{}, fakeClient)
		mgr.WithReadOnly()
		tr := &testReconciler{
			lifecycleManager: mgr,
		}

		// Act
		cfg := &rest.Config{}
		provider := pmtesting.NewFakeProvider(cfg)
		mmanager, err := mcmanager.New(cfg, provider, mcmanager.Options{})
		assert.NoError(t, err)
		err = mgr.SetupWithManager(mmanager, 0, "testReconciler", instance, "test", tr, log.Logger)

		// Assert
		assert.Error(t, err)
	})
	t.Run("Should fail setup with invalid config", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementingSpreadReconciles{}
		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr, log := createLifecycleManager([]subroutine.Subroutine{}, fakeClient)
		mgr.WithReadOnly()
		mgr.WithSpreadingReconciles()
		tr := &testReconciler{
			lifecycleManager: mgr,
		}

		// Act
		cfg := &rest.Config{}
		provider := pmtesting.NewFakeProvider(cfg)
		mmanager, err := mcmanager.New(cfg, provider, mcmanager.Options{})
		assert.NoError(t, err)
		err = mgr.SetupWithManager(mmanager, 0, "testReconciler", instance, "test", tr, log.Logger)

		// Assert
		assert.Error(t, err)
	})

	errorMessage := "oh nose"
	t.Run("Prepare Context", func(t *testing.T) {
		t.Run("Sets a context that can be used in the subroutine", func(t *testing.T) {
			// Arrange
			ctx := context.Background()

			fakeClient := pmtesting.CreateFakeClient(t, testApiObject)

			lm, _ := createLifecycleManager([]subroutine.Subroutine{pmtesting.ContextValueSubroutine{}}, fakeClient)
			lm = lm.WithPrepareContextFunc(func(ctx context.Context, instance runtimeobject.RuntimeObject) (context.Context, operrors.OperatorError) {
				return context.WithValue(ctx, pmtesting.ContextValueKey, "valueFromContext"), nil
			})
			tr := &testReconciler{lifecycleManager: lm}
			req := mcreconcile.Request{
				Request: controllerruntime.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: namespace}},
			}
			result, err := tr.Reconcile(ctx, req)

			// Then
			assert.NotNil(t, ctx)
			assert.NotNil(t, result)
			assert.NoError(t, err)

			err = fakeClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: namespace}, testApiObject)
			assert.NoError(t, err)
			assert.Equal(t, "valueFromContext", testApiObject.Status.Some)
		})

		t.Run("Handles the errors correctly", func(t *testing.T) {
			// Arrange
			ctx := context.Background()

			fakeClient := pmtesting.CreateFakeClient(t, testApiObject)

			lm, _ := createLifecycleManager([]subroutine.Subroutine{pmtesting.ContextValueSubroutine{}}, fakeClient)
			lm = lm.WithPrepareContextFunc(func(ctx context.Context, instance runtimeobject.RuntimeObject) (context.Context, operrors.OperatorError) {
				return nil, operrors.NewOperatorError(goerrors.New(errorMessage), true, false)
			})
			tr := &testReconciler{lifecycleManager: lm}
			request := mcreconcile.Request{
				Request: controllerruntime.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: namespace}},
			}

			result, err := tr.Reconcile(ctx, request)

			// Then
			assert.NotNil(t, ctx)
			assert.NotNil(t, result)
			assert.Error(t, err)
		})
	})
}

// Test LifecycleManager.WithConditionManagement
func TestLifecycleManager_WithConditionManagement(t *testing.T) {
	// Given
	fakeClient := pmtesting.CreateFakeClient(t, &pmtesting.TestApiObject{})
	clusterGetter := &pmtesting.FakeManager{Client: fakeClient}
	_, log := createLifecycleManager([]subroutine.Subroutine{}, fakeClient)

	// When
	l := NewLifecycleManager([]subroutine.Subroutine{}, "test-operator", "test-controller", clusterGetter, log.Logger).WithConditionManagement()

	// Then
	assert.True(t, true, l.ConditionsManager() != nil)
}

type testReconciler struct {
	lifecycleManager *LifecycleManager
}

func (r *testReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (controllerruntime.Result, error) {
	return r.lifecycleManager.Reconcile(ctx, req, &pmtesting.TestApiObject{})
}

func createLifecycleManager(subroutines []subroutine.Subroutine, client ctrlruntimeclient.Client) (*LifecycleManager, *testlogger.TestLogger) {
	log := testlogger.New()
	clusterGetter := &pmtesting.FakeManager{Client: client}
	m := NewLifecycleManager(subroutines, "test-operator", "test-controller", clusterGetter, log.Logger)
	return m, log
}
