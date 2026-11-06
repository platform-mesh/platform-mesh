//go:build integration

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

// Package subroutines_test runs the subroutines lifecycle against a real API
// server (a kcp workspace) with a test CRD, through the public
// go.platform-mesh.io/subroutines packages only.
package subroutines_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"
	"go.platform-mesh.io/subroutines/spread"
	"go.platform-mesh.io/tests/framework/fixtures"
	"go.platform-mesh.io/tests/framework/kcp"
	"go.platform-mesh.io/tests/framework/scheme"
	"go.platform-mesh.io/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/kcp-dev/multicluster-provider/envtest"
	"github.com/kcp-dev/sdk/apis/core"

	_ "embed"
)

//go:embed testdata/testobjects.test.io.yaml
var testObjectCRD []byte

const (
	testNamespace = "default"
	testFinalizer = "test.io/integration"
)

var testGV = schema.GroupVersion{Group: "test.io", Version: "v1alpha1"}

// testObject is the Go type of the testobjects.test.io CRD (a copy of the
// lifecycle package's unit-test type, which is not exported).
type testObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              testSpec   `json:"spec,omitempty"`
	Status            testStatus `json:"status,omitempty"`
}

type testSpec struct {
	Value string `json:"value,omitempty"`
}

type testStatus struct {
	Conditions        []metav1.Condition `json:"conditions,omitempty"`
	Initializers      []string           `json:"initializers,omitempty"`
	Terminators       []string           `json:"terminators,omitempty"`
	ObservedGen       int64              `json:"observedGeneration,omitempty"`
	NextReconcileTime metav1.Time        `json:"nextReconcileTime,omitempty"`
}

func (o *testObject) DeepCopyObject() runtime.Object {
	cp := *o
	om := o.ObjectMeta
	om.DeepCopyInto(&cp.ObjectMeta)
	cp.Status.Conditions = make([]metav1.Condition, len(o.Status.Conditions))
	for i := range o.Status.Conditions {
		o.Status.Conditions[i].DeepCopyInto(&cp.Status.Conditions[i])
	}
	if o.Status.Conditions == nil {
		cp.Status.Conditions = nil
	}
	cp.Status.Initializers = append([]string(nil), o.Status.Initializers...)
	cp.Status.Terminators = append([]string(nil), o.Status.Terminators...)
	o.Status.NextReconcileTime.DeepCopyInto(&cp.Status.NextReconcileTime)
	return &cp
}

// conditions.Manager and spread.Manager work through these accessors.
func (o *testObject) GetConditions() []metav1.Condition   { return o.Status.Conditions }
func (o *testObject) SetConditions(c []metav1.Condition)  { o.Status.Conditions = c }
func (o *testObject) GetObservedGeneration() int64        { return o.Status.ObservedGen }
func (o *testObject) SetObservedGeneration(g int64)       { o.Status.ObservedGen = g }
func (o *testObject) GetNextReconcileTime() metav1.Time   { return o.Status.NextReconcileTime }
func (o *testObject) SetNextReconcileTime(ts metav1.Time) { o.Status.NextReconcileTime = ts }

// fakeManager is the smallest mcmanager.Manager the lifecycle needs: it only
// resolves the cluster client. Every other method panics (nil embedded interface).
type fakeManager struct {
	mcmanager.Manager
	cl ctrlruntimeclient.Client
}

func (f *fakeManager) ClusterFromContext(context.Context) (cluster.Cluster, error) {
	return &fakeCluster{cl: f.cl}, nil
}

type fakeCluster struct {
	cluster.Cluster
	cl ctrlruntimeclient.Client
}

func (f *fakeCluster) GetClient() ctrlruntimeclient.Client { return f.cl }

// finalizerSub is a subroutine with Process, Finalize and a finalizer.
type finalizerSub struct {
	name       string
	processFn  func(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error)
	finalizeFn func(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error)
	finalizers []string
}

func (s *finalizerSub) GetName() string { return s.name }
func (s *finalizerSub) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return s.processFn(ctx, obj)
}
func (s *finalizerSub) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	return s.finalizeFn(ctx, obj)
}
func (s *finalizerSub) Finalizers(ctrlruntimeclient.Object) []string { return s.finalizers }

// setup creates a workspace with the TestObject CRD and returns a client for it.
func setup(t *testing.T) ctrlruntimeclient.Client {
	t.Helper()
	env := kcp.Server(t)
	_, path := env.Workspace(t, core.RootCluster.Path(), envtest.WithNamePrefix("subroutines"))

	s := scheme.New(t)
	s.AddKnownTypeWithName(testGV.WithKind("TestObject"), &testObject{})
	metav1.AddToGroupVersion(s, testGV)
	cl := env.Client(t, path, s)

	fixtures.Apply(t, cl, fixtures.Decode(t, testObjectCRD)[0])
	wait.CRDEstablished(t, cl, "testobjects.test.io")
	return cl
}

func TestIntegration_FullLifecycle(t *testing.T) {
	cl := setup(t)
	ctx := t.Context()

	processCalled := false
	finalizeCalled := false
	sub := &finalizerSub{
		name: "integration-sub",
		processFn: func(context.Context, ctrlruntimeclient.Object) (subroutines.Result, error) {
			processCalled = true
			return subroutines.OK(), nil
		},
		finalizeFn: func(context.Context, ctrlruntimeclient.Object) (subroutines.Result, error) {
			finalizeCalled = true
			return subroutines.OK(), nil
		},
		finalizers: []string{testFinalizer},
	}

	lc := lifecycle.New(&fakeManager{cl: cl}, "integration-controller", func() ctrlruntimeclient.Object { return &testObject{} }, sub).
		WithConditions(conditions.NewManager()).
		WithSpread(spread.NewManager(
			spread.WithMinDuration(1*time.Hour),
			spread.WithMaxDuration(2*time.Hour),
		))

	key := types.NamespacedName{Name: "integration-test", Namespace: testNamespace}
	req := mcreconcile.Request{Request: reconcile.Request{NamespacedName: key}}

	// Create the test object. The CRD's REST mapping can lag behind Established; retry.
	obj := &testObject{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
	wait.Eventually(t, func() (bool, string) {
		if err := cl.Create(ctx, obj.DeepCopyObject().(ctrlruntimeclient.Object)); err != nil {
			return false, err.Error()
		}
		return true, ""
	}, wait.Timeout, wait.Tick, "creating TestObject")

	// First reconcile adds the finalizer and stops; the watch event would
	// trigger the next reconcile.
	result, err := lc.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assert.False(t, processCalled, "process must wait for the reconcile after the finalizer was added")

	fetched := &testObject{}
	require.NoError(t, cl.Get(ctx, key, fetched))
	assert.Contains(t, fetched.Finalizers, testFinalizer)

	// Second reconcile processes, sets conditions and records the next
	// reconcile time in status. It does not requeue itself: the status patch
	// triggers the next reconcile, which spread turns into a delayed requeue.
	result, err = lc.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.True(t, processCalled)
	assert.Equal(t, reconcile.Result{}, result)

	fetched = &testObject{}
	require.NoError(t, cl.Get(ctx, key, fetched))
	readyCond := meta.FindStatusCondition(fetched.Status.Conditions, conditions.ReadyCondition)
	require.NotNil(t, readyCond, "Ready condition should be persisted in status")
	assert.Equal(t, metav1.ConditionTrue, readyCond.Status)
	assert.Equal(t, fetched.Generation, fetched.Status.ObservedGen, "spread should record the observed generation")
	assert.False(t, fetched.Status.NextReconcileTime.IsZero(), "spread should persist the next reconcile time")

	// A third reconcile before the next reconcile time is skipped by spread.
	processCalled = false
	result, err = lc.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.False(t, processCalled, "spread should skip a reconcile that is not yet due")
	assert.Greater(t, result.RequeueAfter, 30*time.Minute, "spread should requeue at the next reconcile time (1h-2h out)")

	// Delete the object; the finalizer keeps it around.
	require.NoError(t, cl.Delete(ctx, fetched))

	result, err = lc.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assert.True(t, finalizeCalled)

	// Object should be gone (finalizer removed, the API server deletes it).
	err = cl.Get(ctx, key, &testObject{})
	assert.True(t, apierrors.IsNotFound(err), "object should be deleted after finalizer removal, got %v", err)
}
