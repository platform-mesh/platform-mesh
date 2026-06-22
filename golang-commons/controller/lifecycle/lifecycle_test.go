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

package lifecycle

import (
	"context"
	goerrors "errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/conditions"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/mocks"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	pmtesting "go.platform-mesh.io/golang-commons/controller/testSupport"
	operrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/golang-commons/sentry"
)

func TestLifecycle(t *testing.T) {
	namespace := "bar"
	name := "foo"
	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}
	testApiObject := &pmtesting.TestApiObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	ctx := context.Background()
	logcfg := logger.DefaultConfig()
	logcfg.NoJSON = true
	log, err := logger.New(logcfg)
	assert.NoError(t, err)

	t.Run("Lifecycle with a not found object", func(t *testing.T) {
		// Arrange
		fakeClient := pmtesting.CreateFakeClient(t, &pmtesting.TestApiObject{})

		mgr := &pmtesting.TestLifecycleManager{Logger: log}

		// Act
		result, err := Reconcile(ctx, request.NamespacedName, &pmtesting.TestApiObject{}, fakeClient, mgr)

		// Assert
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.NoError(t, err)
	})
	t.Run("Lifecycle with a finalizer - add finalizer", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FinalizerSubroutine{
				Client: fakeClient,
			}},
		}

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Equal(t, 1, len(instance.Finalizers))
	})
	t.Run("Lifecycle with a finalizer - finalization", func(t *testing.T) {
		// Arrange
		now := &metav1.Time{Time: time.Now()}
		finalizers := []string{pmtesting.SubroutineFinalizer}
		instance := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         namespace,
				DeletionTimestamp: now,
				Finalizers:        finalizers,
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FinalizerSubroutine{
				Client: fakeClient,
			},
		}}

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Equal(t, 0, len(instance.Finalizers))
	})
	t.Run("Lifecycle with a finalizer - finalization(requeue)", func(t *testing.T) {
		// Arrange
		now := &metav1.Time{Time: time.Now()}
		finalizers := []string{pmtesting.SubroutineFinalizer}
		instance := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         namespace,
				DeletionTimestamp: now,
				Finalizers:        finalizers,
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FinalizerSubroutine{
				Client:       fakeClient,
				RequeueAfter: 1 * time.Second,
			},
		}}

		// Act
		res, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Equal(t, 1, len(instance.Finalizers))
		assert.Equal(t, 1*time.Second, res.RequeueAfter)
	})
	t.Run("Lifecycle with a finalizer - finalization(requeueAfter)", func(t *testing.T) {
		// Arrange
		now := &metav1.Time{Time: time.Now()}
		finalizers := []string{pmtesting.SubroutineFinalizer}
		instance := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         namespace,
				DeletionTimestamp: now,
				Finalizers:        finalizers,
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FinalizerSubroutine{
				Client:       fakeClient,
				RequeueAfter: 2 * time.Second,
			},
		}}

		// Act
		res, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)
		assert.NoError(t, err)

		assert.Equal(t, 1, len(instance.Finalizers))

		assert.Equal(t, 2*time.Second, res.RequeueAfter)
	})
	t.Run("Lifecycle with a finalizer - skip finalization if the finalizer is not in there", func(t *testing.T) {
		// Arrange
		now := &metav1.Time{Time: time.Now()}
		finalizers := []string{"other-finalizer"}
		instance := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         namespace,
				DeletionTimestamp: now,
				Finalizers:        finalizers,
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FinalizerSubroutine{
				Client: fakeClient,
			},
		}}

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Equal(t, 1, len(instance.Finalizers))
	})
	t.Run("Lifecycle with a finalizer - failing finalization subroutine", func(t *testing.T) {
		// Arrange
		now := &metav1.Time{Time: time.Now()}
		finalizers := []string{pmtesting.SubroutineFinalizer}
		instance := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         namespace,
				DeletionTimestamp: now,
				Finalizers:        finalizers,
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FinalizerSubroutine{
				Client: fakeClient,
				Err:    fmt.Errorf("some error"),
			},
		}}

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.Error(t, err)
		assert.Equal(t, 1, len(instance.Finalizers))
	})
	t.Run("Lifecycle removes terminator when in deletion", func(t *testing.T) {
		// Arrange: instance in deletion with a finalizer no subroutine
		// manages(simulating a LogicalCluster), so it stays and the object
		// remains in the fake client for the terminator status patch
		terminatorName := "root:security"
		now := &metav1.Time{Time: time.Now()}
		instance := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         namespace,
				DeletionTimestamp: now,
				Finalizers:        []string{"test-keeping-finalizer"},
			},
			Status: pmtesting.TestStatus{
				Terminators: []string{terminatorName, "other:terminator"},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{
			Logger:         log,
			SubroutinesArr: []subroutine.Subroutine{},
		}
		mgr.WithTerminator(terminatorName)

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		// Assert
		assert.NoError(t, err)
		assert.Equal(t, []string{"other:terminator"}, instance.Status.Terminators)

		serverObject := &pmtesting.TestApiObject{}
		getErr := fakeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, serverObject)
		assert.NoError(t, getErr)
		assert.Equal(t, []string{"other:terminator"}, serverObject.Status.Terminators)
	})
	t.Run("Lifecycle removes initializer when not in deletion", func(t *testing.T) {
		// Arrange: instance without deletion timestamp, with initializers in status
		initializerName := "root:security"
		instance := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Status: pmtesting.TestStatus{
				Initializers: []string{initializerName, "other:initializer"},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{
			Logger:         log,
			SubroutinesArr: []subroutine.Subroutine{},
		}
		mgr.WithInitializer(initializerName)

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		// Assert
		assert.NoError(t, err)
		assert.Equal(t, []string{"other:initializer"}, instance.Status.Initializers)

		serverObject := &pmtesting.TestApiObject{}
		getErr := fakeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, serverObject)
		assert.NoError(t, getErr)
		assert.Equal(t, []string{"other:initializer"}, serverObject.Status.Initializers)
	})
	t.Run("Lifecycle without changing status", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Status: pmtesting.TestStatus{Some: "string"},
		}
		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, SubroutinesArr: []subroutine.Subroutine{}}

		// Act
		result, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		// Assert
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
	t.Run("Lifecycle with changing status", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Status: pmtesting.TestStatus{Some: "string"},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.ChangeStatusSubroutine{
				Client: fakeClient,
			},
		}}

		// Act
		result, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		// Assert
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.NoError(t, err)

		serverObject := &pmtesting.TestApiObject{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, serverObject)
		assert.NoError(t, err)
		assert.Equal(t, serverObject.Status.Some, "other string")
	})
	t.Run("Lifecycle with spread reconciles", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{
					Some:               "string",
					ObservedGeneration: 0,
				},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.ChangeStatusSubroutine{
				Client: fakeClient,
			},
		}}
		mgr.WithSpreadingReconciles()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Equal(t, instance.Generation, instance.Status.ObservedGeneration)
	})
	t.Run("Lifecycle with spread reconciles on deleted object", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:              name,
					Namespace:         namespace,
					Generation:        2,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{pmtesting.ChangeStatusSubroutineFinalizer},
				},
				Status: pmtesting.TestStatus{
					Some:               "string",
					ObservedGeneration: 2,
					NextReconcileTime:  metav1.Time{Time: time.Now().Add(2 * time.Hour)},
				},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.ChangeStatusSubroutine{
				Client: fakeClient,
			},
		}}
		mgr.WithSpreadingReconciles()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)
		assert.NoError(t, err)
		assert.Len(t, instance.Finalizers, 0)
	})
	t.Run("Lifecycle with spread reconciles skips if the generation is the same", func(t *testing.T) {
		// Arrange
		nextReconcileTime := metav1.NewTime(time.Now().Add(1 * time.Hour))
		instance := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{
					Some:               "string",
					ObservedGeneration: 1,
					NextReconcileTime:  nextReconcileTime,
				},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FailureScenarioSubroutine{RequeAfter: false},
		}}
		mgr.WithSpreadingReconciles()

		// Act
		result, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Equal(t, int64(1), instance.Status.ObservedGeneration)
		assert.GreaterOrEqual(t, 12*time.Hour, result.RequeueAfter)
	})
	t.Run("Lifecycle with spread reconciles and processing fails (no-retry)", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{
					Some:               "string",
					ObservedGeneration: 0,
				},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FailureScenarioSubroutine{Retry: false, RequeAfter: false},
		}}
		mgr.WithSpreadingReconciles()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Equal(t, int64(1), instance.Status.ObservedGeneration)
	})
	t.Run("Lifecycle with spread reconciles and processing fails (retry)", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{
					Some:               "string",
					ObservedGeneration: 0,
				},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FailureScenarioSubroutine{Retry: true, RequeAfter: false},
		}}
		mgr.WithSpreadingReconciles()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.Error(t, err)
		assert.Equal(t, int64(0), instance.Status.ObservedGeneration)
	})
	t.Run("Lifecycle with spread reconciles and processing needs requeue", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{
					Some:               "string",
					ObservedGeneration: 0,
				},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)
		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FailureScenarioSubroutine{RequeAfter: true},
		}}
		mgr.WithSpreadingReconciles()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Equal(t, int64(0), instance.Status.ObservedGeneration)
	})
	t.Run("Lifecycle with spread reconciles and processing needs requeueAfter", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{
					Some:               "string",
					ObservedGeneration: 0,
				},
			},
		}
		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FailureScenarioSubroutine{RequeAfter: true},
		}}
		mgr.WithSpreadingReconciles()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Equal(t, int64(0), instance.Status.ObservedGeneration)
	})
	t.Run("Lifecycle with spread not implementing the interface", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.NotImplementingSpreadReconciles{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  namespace,
				Generation: 1,
			},
			Status: pmtesting.TestStatus{
				Some:               "string",
				ObservedGeneration: 0,
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.ChangeStatusSubroutine{
				Client: fakeClient,
			},
		}}
		mgr.WithSpreadingReconciles()

		// Act
		assert.Panics(t, func() {
			_, _ = Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)
		})
	})
	t.Run("Lifecycle with spread reconciles and refresh label", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
					Labels:     map[string]string{"platform-mesh.io/refresh-reconcile": "true"},
				},
				Status: pmtesting.TestStatus{
					Some:               "string",
					ObservedGeneration: 1,
				},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.ChangeStatusSubroutine{
				Client: fakeClient,
			},
		}}
		mgr.WithSpreadingReconciles()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Equal(t, int64(1), instance.Status.ObservedGeneration)

		serverObject := &pmtesting.ImplementingSpreadReconciles{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, serverObject)
		assert.NoError(t, err)
		assert.Equal(t, serverObject.Status.Some, "other string")
		_, ok := serverObject.Labels["platform-mesh.io/refresh-reconcile"]
		assert.False(t, ok)
	})
	t.Run("Should handle a client error", func(t *testing.T) {
		// Arrange
		testErr := fmt.Errorf("test error")

		// Act
		result, err := HandleClientError("test", log, testErr, true, sentry.Tags{})

		// Assert
		assert.Error(t, err)
		assert.Equal(t, testErr, err)
		assert.Equal(t, ctrl.Result{}, result)
	})
	t.Run("Lifecycle with manage conditions reconciles w/o subroutines", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementConditions{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{}}
		mgr.WithConditionManagement()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Len(t, instance.Status.Conditions, 1)
		assert.Equal(t, instance.Status.Conditions[0].Type, conditions.ConditionReady)
		assert.Equal(t, instance.Status.Conditions[0].Status, metav1.ConditionTrue)
		assert.Equal(t, instance.Status.Conditions[0].Message, "The resource is ready")
	})
	t.Run("Lifecycle with manage conditions reconciles with subroutine", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementConditions{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.ChangeStatusSubroutine{
				Client: fakeClient,
			},
		}}
		mgr.WithConditionManagement()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		require.Len(t, instance.Status.Conditions, 2)
		assert.Equal(t, conditions.ConditionReady, instance.Status.Conditions[0].Type)
		assert.Equal(t, metav1.ConditionTrue, instance.Status.Conditions[0].Status)
		assert.Equal(t, "The resource is ready", instance.Status.Conditions[0].Message)
		assert.Equal(t, "changeStatus_Ready", instance.Status.Conditions[1].Type)
		assert.Equal(t, metav1.ConditionTrue, instance.Status.Conditions[1].Status)
		assert.Equal(t, "The subroutine is complete", instance.Status.Conditions[1].Message)
	})
	t.Run("Lifecycle with manage conditions reconciles with subroutine failing Status update", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementConditions{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.ChangeStatusSubroutine{
				Client: fakeClient,
			},
		}}
		mgr.WithConditionManagement()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Len(t, instance.Status.Conditions, 2)
		assert.Equal(t, conditions.ConditionReady, instance.Status.Conditions[0].Type)
		assert.Equal(t, metav1.ConditionTrue, instance.Status.Conditions[0].Status)
		assert.Equal(t, "The resource is ready", instance.Status.Conditions[0].Message)
		assert.Equal(t, "changeStatus_Ready", instance.Status.Conditions[1].Type)
		assert.Equal(t, metav1.ConditionTrue, instance.Status.Conditions[1].Status)
		assert.Equal(t, "The subroutine is complete", instance.Status.Conditions[1].Message)
	})
	t.Run("Lifecycle with manage conditions finalizes with multiple subroutines partially succeeding", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementConditions{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:              name,
					Namespace:         namespace,
					Generation:        1,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{pmtesting.FailureScenarioSubroutineFinalizer, pmtesting.ChangeStatusSubroutineFinalizer},
				},
				Status: pmtesting.TestStatus{},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FailureScenarioSubroutine{},
			pmtesting.ChangeStatusSubroutine{Client: fakeClient}}}
		mgr.WithConditionManagement()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.Error(t, err)
		require.Len(t, instance.Status.Conditions, 3)
	})
	t.Run("Lifecycle with manage conditions reconciles with Error subroutine (no-retry)", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementConditions{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FailureScenarioSubroutine{RequeAfter: false},
		}}
		mgr.WithConditionManagement()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Len(t, instance.Status.Conditions, 2)
	})
	t.Run("Lifecycle with manage conditions reconciles with Error subroutine (retry)", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementConditions{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FailureScenarioSubroutine{Retry: true, RequeAfter: false},
		}}
		mgr.WithConditionManagement()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.Error(t, err)
		assert.Len(t, instance.Status.Conditions, 2)
	})
	t.Run("Lifecycle with spread reconciles and manage conditions and processing fails (no-retry)", func(t *testing.T) {
		// Arrange
		instance := &pmtesting.ImplementConditionsAndSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Generation: 1,
				},
				Status: pmtesting.TestStatus{
					Some:               "string",
					ObservedGeneration: 0,
				},
			},
		}

		fakeClient := pmtesting.CreateFakeClient(t, instance)

		mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
			pmtesting.FailureScenarioSubroutine{RequeAfter: false},
		}}
		mgr.WithSpreadingReconciles()
		mgr.WithConditionManagement()

		// Act
		_, err := Reconcile(ctx, request.NamespacedName, instance, fakeClient, mgr)

		assert.NoError(t, err)
		assert.Len(t, instance.Status.Conditions, 2)
	})
	errorMessage := "oh nose"
	t.Run("Should handle an operator error without retry", func(t *testing.T) {
		// Arrange
		testLog := testlogger.New()

		// Act
		result, err := HandleOperatorError(ctx, operrors.NewOperatorError(goerrors.New(errorMessage), false, false), "handle op error", true, testLog.Logger)

		// Assert
		assert.Nil(t, err)
		assert.NotNil(t, result)

		errorMessages, err := testLog.GetErrorMessages()
		assert.NoError(t, err)
		assert.Equal(t, errorMessage, *errorMessages[0].Error)
	})
	t.Run("Prepare Context", func(t *testing.T) {
		t.Run("Sets a context that can be used in the subroutine", func(t *testing.T) {
			// Arrange
			ctx := context.Background()

			fakeClient := pmtesting.CreateFakeClient(t, testApiObject)

			mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
				pmtesting.ContextValueSubroutine{},
			}}

			mgr = mgr.WithPrepareContextFunc(func(ctx context.Context, instance runtimeobject.RuntimeObject) (context.Context, operrors.OperatorError) {
				return context.WithValue(ctx, pmtesting.ContextValueKey, "valueFromContext"), nil
			})
			result, err := Reconcile(ctx, types.NamespacedName{Name: name, Namespace: namespace}, testApiObject, fakeClient, mgr)

			// Then
			assert.NotNil(t, ctx)
			assert.NotNil(t, result)
			assert.NoError(t, err)

			err = fakeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, testApiObject)
			assert.NoError(t, err)
			assert.Equal(t, "valueFromContext", testApiObject.Status.Some)
		})

		t.Run("Handles the errors correctly", func(t *testing.T) {
			// Arrange
			ctx := context.Background()

			fakeClient := pmtesting.CreateFakeClient(t, testApiObject)

			mgr := &pmtesting.TestLifecycleManager{Logger: log, ShouldReconcile: true, SubroutinesArr: []subroutine.Subroutine{
				pmtesting.ContextValueSubroutine{},
			}}

			mgr = mgr.WithPrepareContextFunc(func(ctx context.Context, instance runtimeobject.RuntimeObject) (context.Context, operrors.OperatorError) {
				return nil, operrors.NewOperatorError(goerrors.New(errorMessage), true, false)
			})
			result, err := Reconcile(ctx, types.NamespacedName{Name: name, Namespace: namespace}, testApiObject, fakeClient, mgr)

			// Then
			assert.NotNil(t, ctx)
			assert.NotNil(t, result)
			assert.Error(t, err)
		})
	})
}

func TestUpdateStatus(t *testing.T) {
	clientMock := new(mocks.Client)
	subresourceClient := new(mocks.SubResourceWriter)

	logcfg := logger.DefaultConfig()
	logcfg.NoJSON = true
	log, err := logger.New(logcfg)
	assert.NoError(t, err)

	t.Run("Test UpdateStatus with no changes", func(t *testing.T) {
		original := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				Status: pmtesting.TestStatus{
					Some: "string",
				},
			}}

		// When
		err := updateStatus(context.Background(), clientMock, original, original, log, true, nil)

		// Then
		assert.NoError(t, err)
	})

	t.Run("Test UpdateStatus with update error", func(t *testing.T) {
		original := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				Status: pmtesting.TestStatus{
					Some: "string",
				},
			}}
		current := &pmtesting.ImplementingSpreadReconciles{
			TestApiObject: pmtesting.TestApiObject{
				Status: pmtesting.TestStatus{
					Some: "string1",
				},
			}}

		clientMock.EXPECT().Status().Return(subresourceClient)
		subresourceClient.EXPECT().Update(mock.Anything, mock.Anything, mock.Anything).
			Return(errors.NewBadRequest("internal error"))

		// When
		err := updateStatus(context.Background(), clientMock, original, current, log, true, nil)

		// Then
		assert.Error(t, err)
		assert.Equal(t, "internal error", err.Error())
	})

	t.Run("Test UpdateStatus with no status object (original)", func(t *testing.T) {
		original := &pmtesting.TestNoStatusApiObject{}
		current := &pmtesting.ImplementConditions{}
		// When
		err := updateStatus(context.Background(), clientMock, original, current, log, true, nil)

		// Then
		assert.Error(t, err)
		assert.Equal(t, "internal error", err.Error())
	})
	t.Run("Test UpdateStatus with no status object (current)", func(t *testing.T) {
		original := &pmtesting.ImplementConditions{}
		current := &pmtesting.TestNoStatusApiObject{}
		// When
		err := updateStatus(context.Background(), clientMock, original, current, log, true, nil)

		// Then
		assert.Error(t, err)
		assert.Equal(t, "internal error", err.Error())
	})
}

func TestAddFinalizersIfNeeded(t *testing.T) {
	instance := &pmtesting.TestApiObject{ObjectMeta: metav1.ObjectMeta{Name: "instance1"}}
	fakeClient := pmtesting.CreateFakeClient(t, instance)
	sub := pmtesting.FinalizerSubroutine{Client: fakeClient}
	// Should add finalizer
	err := AddFinalizersIfNeeded(context.Background(), fakeClient, instance, []subroutine.Subroutine{sub}, false)
	assert.NoError(t, err)
	assert.Contains(t, instance.Finalizers, pmtesting.SubroutineFinalizer)

	// Should not add if readonly
	instance2 := &pmtesting.TestApiObject{}
	err = AddFinalizersIfNeeded(context.Background(), fakeClient, instance2, []subroutine.Subroutine{sub}, true)
	assert.NoError(t, err)
	assert.NotContains(t, instance2.Finalizers, pmtesting.SubroutineFinalizer)

	// Should not add if deletion timestamp is set
	now := metav1.Now()
	instance3 := &pmtesting.TestApiObject{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now}}
	err = AddFinalizersIfNeeded(context.Background(), fakeClient, instance3, []subroutine.Subroutine{sub}, false)
	assert.NoError(t, err)
}

func TestAddFinalizerIfNeeded(t *testing.T) {
	instance := &pmtesting.TestApiObject{}
	sub := pmtesting.FinalizerSubroutine{}
	// Should add and return true
	added := AddFinalizerIfNeeded(instance, sub)
	assert.True(t, added)
	// Should not add again
	added = AddFinalizerIfNeeded(instance, sub)
	assert.False(t, added)
}

func TestRemoveFinalizerIfNeeded(t *testing.T) {
	instance := &pmtesting.TestApiObject{ObjectMeta: metav1.ObjectMeta{Name: "instance1"}}
	sub := pmtesting.FinalizerSubroutine{}
	AddFinalizerIfNeeded(instance, sub)
	fakeClient := pmtesting.CreateFakeClient(t, instance)
	// Should remove finalizer if not readonly and RequeueAfter == 0
	res := ctrl.Result{}
	err := removeFinalizerIfNeeded(context.Background(), instance, sub, res, false, fakeClient)
	assert.Nil(t, err)
	assert.NotContains(t, instance.Finalizers, pmtesting.SubroutineFinalizer)

	// Should not remove if readonly
	AddFinalizerIfNeeded(instance, sub)
	err = removeFinalizerIfNeeded(context.Background(), instance, sub, res, true, fakeClient)
	assert.Nil(t, err)
	assert.Contains(t, instance.Finalizers, pmtesting.SubroutineFinalizer)

	// Should not remove if RequeueAfter > 0
	res = ctrl.Result{RequeueAfter: 1}
	err = removeFinalizerIfNeeded(context.Background(), instance, sub, res, false, fakeClient)
	assert.Nil(t, err)
}

func TestRemoveTerminatorIfNeeded(t *testing.T) {
	terminatorToRemove := "root:security"
	instance := &pmtesting.TestApiObject{
		ObjectMeta: metav1.ObjectMeta{Name: "instance1", Namespace: "default"},
		Status: pmtesting.TestStatus{
			Terminators: []string{terminatorToRemove, "other:terminator"},
		},
	}
	fakeClient := pmtesting.CreateFakeClient(t, instance)

	t.Run("removes terminator from status", func(t *testing.T) {
		err := removeTerminator(context.Background(), instance, fakeClient, terminatorToRemove)
		assert.NoError(t, err)
		assert.Equal(t, []string{"other:terminator"}, instance.Status.Terminators)

		serverObject := &pmtesting.TestApiObject{}
		getErr := fakeClient.Get(context.Background(), client.ObjectKey{Name: "instance1", Namespace: "default"}, serverObject)
		assert.NoError(t, getErr)
		assert.Equal(t, []string{"other:terminator"}, serverObject.Status.Terminators)
	})

	t.Run("does nothing when terminator is empty", func(t *testing.T) {
		instance3 := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{Name: "instance3", Namespace: "default"},
			Status:     pmtesting.TestStatus{Terminators: []string{terminatorToRemove}},
		}
		fakeClient3 := pmtesting.CreateFakeClient(t, instance3)
		err := removeTerminator(context.Background(), instance3, fakeClient3, "")
		assert.NoError(t, err)
		assert.Equal(t, []string{terminatorToRemove}, instance3.Status.Terminators)
	})

	t.Run("does nothing when terminator not in list", func(t *testing.T) {
		instance5 := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{Name: "instance5", Namespace: "default"},
			Status:     pmtesting.TestStatus{Terminators: []string{"other:terminator"}},
		}
		fakeClient5 := pmtesting.CreateFakeClient(t, instance5)
		err := removeTerminator(context.Background(), instance5, fakeClient5, terminatorToRemove)
		assert.NoError(t, err)
		assert.Equal(t, []string{"other:terminator"}, instance5.Status.Terminators)
	})

	t.Run("does nothing when object has no terminators", func(t *testing.T) {
		instance6 := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{Name: "instance6", Namespace: "default"},
			Status:     pmtesting.TestStatus{},
		}
		fakeClient6 := pmtesting.CreateFakeClient(t, instance6)
		err := removeTerminator(context.Background(), instance6, fakeClient6, terminatorToRemove)
		assert.NoError(t, err)
		assert.Nil(t, instance6.Status.Terminators)
	})
}

func TestRemoveInitializerIfNeeded(t *testing.T) {
	initializerToRemove := "root:security"
	instance := &pmtesting.TestApiObject{
		ObjectMeta: metav1.ObjectMeta{Name: "instance1", Namespace: "default"},
		Status: pmtesting.TestStatus{
			Initializers: []string{initializerToRemove, "other:initializer"},
		},
	}
	fakeClient := pmtesting.CreateFakeClient(t, instance)

	t.Run("removes initializer from status", func(t *testing.T) {
		err := removeInitializer(context.Background(), instance, fakeClient, initializerToRemove)
		assert.NoError(t, err)
		assert.Equal(t, []string{"other:initializer"}, instance.Status.Initializers)

		serverObject := &pmtesting.TestApiObject{}
		getErr := fakeClient.Get(context.Background(), client.ObjectKey{Name: "instance1", Namespace: "default"}, serverObject)
		assert.NoError(t, getErr)
		assert.Equal(t, []string{"other:initializer"}, serverObject.Status.Initializers)
	})

	t.Run("does nothing when initializer is empty", func(t *testing.T) {
		instance3 := &pmtesting.TestApiObject{
			ObjectMeta: metav1.ObjectMeta{Name: "instance3", Namespace: "default"},
			Status:     pmtesting.TestStatus{Initializers: []string{initializerToRemove}},
		}
		fakeClient3 := pmtesting.CreateFakeClient(t, instance3)
		err := removeInitializer(context.Background(), instance3, fakeClient3, "")
		assert.NoError(t, err)
		assert.Equal(t, []string{initializerToRemove}, instance3.Status.Initializers)
	})
}

func TestContainsFinalizer(t *testing.T) {
	instance := &pmtesting.TestApiObject{}
	sub := pmtesting.FinalizerSubroutine{}
	assert.False(t, containsFinalizer(instance, sub.Finalizers(instance)))
	AddFinalizerIfNeeded(instance, sub)
	assert.True(t, containsFinalizer(instance, sub.Finalizers(instance)))
}

func TestMarkResourceAsFinal(t *testing.T) {
	instance := &pmtesting.ImplementingSpreadReconciles{}
	logcfg := logger.DefaultConfig()
	logcfg.NoJSON = true
	log, _ := logger.New(logcfg)
	conds := []metav1.Condition{}
	mgr := &pmtesting.TestLifecycleManager{Logger: log}
	MarkResourceAsFinal(instance, log, conds, metav1.ConditionTrue, mgr)
	assert.Equal(t, instance.Status.ObservedGeneration, instance.Generation)
}

func TestHandleClientError(t *testing.T) {
	log := testlogger.New().Logger
	result, err := HandleClientError("msg", log, fmt.Errorf("err"), true, sentry.Tags{})
	assert.Error(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestHandleOperatorError(t *testing.T) {
	log := testlogger.New().Logger
	opErr := operrors.NewOperatorError(fmt.Errorf("err"), false, false)
	result, err := HandleOperatorError(context.Background(), opErr, "msg", true, log)
	assert.Nil(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	ctx := sentry.ContextWithSentryTags(context.Background(), sentry.Tags{"test": "tag"})
	opErr = operrors.NewOperatorError(fmt.Errorf("err"), true, true)
	result, err = HandleOperatorError(ctx, opErr, "msg", true, log)
	assert.Error(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestValidateInterfaces(t *testing.T) {
	log := testlogger.New().Logger
	instance := &pmtesting.ImplementingSpreadReconciles{}
	mgr := &pmtesting.TestLifecycleManager{Logger: log}
	err := ValidateInterfaces(instance, log, mgr)
	assert.NoError(t, err)
}
