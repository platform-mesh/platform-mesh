package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// --- Test CRD ---

var testGVK = schema.GroupVersionKind{Group: "test.io", Version: "v1alpha1", Kind: "TestObject"}

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

func (t *testObject) GetObjectKind() schema.ObjectKind { return &t.TypeMeta }
func (t *testObject) DeepCopyObject() runtime.Object {
	cp := *t
	cp.Finalizers = append([]string(nil), t.Finalizers...)
	cp.Status.Conditions = append([]metav1.Condition(nil), t.Status.Conditions...)
	cp.Status.Initializers = append([]string(nil), t.Status.Initializers...)
	cp.Status.Terminators = append([]string(nil), t.Status.Terminators...)
	if t.Labels != nil {
		cp.Labels = make(map[string]string, len(t.Labels))
		for k, v := range t.Labels {
			cp.Labels[k] = v
		}
	}
	if t.Annotations != nil {
		cp.Annotations = make(map[string]string, len(t.Annotations))
		for k, v := range t.Annotations {
			cp.Annotations[k] = v
		}
	}
	return &cp
}

func (t *testObject) GetConditions() []metav1.Condition  { return t.Status.Conditions }
func (t *testObject) SetConditions(c []metav1.Condition) { t.Status.Conditions = c }

func (t *testObject) GetObservedGeneration() int64        { return t.Status.ObservedGen }
func (t *testObject) SetObservedGeneration(g int64)       { t.Status.ObservedGen = g }
func (t *testObject) GetNextReconcileTime() metav1.Time   { return t.Status.NextReconcileTime }
func (t *testObject) SetNextReconcileTime(ts metav1.Time) { t.Status.NextReconcileTime = ts }

// --- Stub Subroutines ---

type processorSub struct {
	name string
	fn   func(ctx context.Context, obj client.Object) (subroutines.Result, error)
}

func (s *processorSub) GetName() string { return s.name }
func (s *processorSub) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return s.fn(ctx, obj)
}

type finalizerSub struct {
	name       string
	processFn  func(ctx context.Context, obj client.Object) (subroutines.Result, error)
	finalizeFn func(ctx context.Context, obj client.Object) (subroutines.Result, error)
	finalizers []string
}

func (s *finalizerSub) GetName() string { return s.name }
func (s *finalizerSub) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return s.processFn(ctx, obj)
}
func (s *finalizerSub) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return s.finalizeFn(ctx, obj)
}
func (s *finalizerSub) Finalizers(client.Object) []string { return s.finalizers }

type fakeErrorReporter struct {
	reported []ErrorInfo
}

func (f *fakeErrorReporter) Report(_ context.Context, _ error, info ErrorInfo) {
	f.reported = append(f.reported, info)
}

// --- Helpers ---

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(testGVK, &testObject{})
	metav1.AddToGroupVersion(s, schema.GroupVersion{Group: "test.io", Version: "v1alpha1"})
	return s
}

func newTestObj(name, ns string) *testObject {
	return &testObject{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "test.io/v1alpha1",
			Kind:       "TestObject",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			UID:        types.UID("test-uid"),
			Generation: 1,
		},
	}
}

func newReq(name, ns string) mcreconcile.Request {
	return mcreconcile.Request{
		Request: reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: ns}},
	}
}

func setupLifecycle(cl client.Client, subs ...subroutines.Subroutine) *Lifecycle {
	mgr := &fakeManager{cl: cl}
	return New(mgr, "test-controller", func() client.Object { return &testObject{} }, subs...)
}

// --- Tests ---

func TestObjectNotFound(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	lc := setupLifecycle(cl)

	result, err := lc.Reconcile(context.Background(), newReq("missing", "default"))
	require.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestSingleProcessor(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn:   func(context.Context, client.Object) (subroutines.Result, error) { return subroutines.OK(), nil },
	}

	lc := setupLifecycle(cl, sub).WithConditions(conditions.NewManager())

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Verify conditions were set.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	readyCond := meta.FindStatusCondition(fetched.Status.Conditions, "Ready")
	require.NotNil(t, readyCond)
	assert.Equal(t, metav1.ConditionTrue, readyCond.Status)
}

func TestProcessorError_StopsChain(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	callOrder := []string{}
	sub1 := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			callOrder = append(callOrder, "step1")
			return subroutines.OK(), errors.New("step1 failed")
		},
	}
	sub2 := &processorSub{
		name: "step2",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			callOrder = append(callOrder, "step2")
			return subroutines.OK(), nil
		},
	}

	reporter := &fakeErrorReporter{}
	lc := setupLifecycle(cl, sub1, sub2).
		WithConditions(conditions.NewManager()).
		WithErrorReporters(reporter)

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "step1 failed")

	// step2 should not have been called.
	assert.Equal(t, []string{"step1"}, callOrder)

	// Error reporter should have been called.
	require.Len(t, reporter.reported, 1)
	assert.Equal(t, "step1", reporter.reported[0].Subroutine)
	assert.Equal(t, ActionProcess, reporter.reported[0].Action)
}

func TestStopWithRequeue_BreaksChain(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	callOrder := []string{}
	sub1 := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			callOrder = append(callOrder, "step1")
			return subroutines.StopWithRequeue(30*time.Second, "rate limited"), nil
		},
	}
	sub2 := &processorSub{
		name: "step2",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			callOrder = append(callOrder, "step2")
			return subroutines.OK(), nil
		},
	}

	lc := setupLifecycle(cl, sub1, sub2).WithConditions(conditions.NewManager())

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, result.RequeueAfter)
	assert.Equal(t, []string{"step1"}, callOrder)

	// Ready should be False/Stopped.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	readyCond := meta.FindStatusCondition(fetched.Status.Conditions, "Ready")
	require.NotNil(t, readyCond)
	assert.Equal(t, metav1.ConditionFalse, readyCond.Status)
}

func TestStop_BreaksChainNoRequeue(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.Stop("precondition failed"), nil
		},
	}

	lc := setupLifecycle(cl, sub).WithConditions(conditions.NewManager())

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestPending_ContinuesChain(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	callOrder := []string{}
	sub1 := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			callOrder = append(callOrder, "step1")
			return subroutines.Pending(10*time.Second, "waiting"), nil
		},
	}
	sub2 := &processorSub{
		name: "step2",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			callOrder = append(callOrder, "step2")
			return subroutines.OK(), nil
		},
	}

	lc := setupLifecycle(cl, sub1, sub2).WithConditions(conditions.NewManager())

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, result.RequeueAfter)
	assert.Equal(t, []string{"step1", "step2"}, callOrder)

	// Ready should be Unknown/Pending.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	readyCond := meta.FindStatusCondition(fetched.Status.Conditions, "Ready")
	require.NotNil(t, readyCond)
	assert.Equal(t, metav1.ConditionUnknown, readyCond.Status)
}

func TestMultiplePending_MinRequeue(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub1 := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.Pending(30*time.Second, "waiting A"), nil
		},
	}
	sub2 := &processorSub{
		name: "step2",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.Pending(10*time.Second, "waiting B"), nil
		},
	}

	lc := setupLifecycle(cl, sub1, sub2)

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, result.RequeueAfter)
}

func TestFinalizeFlow(t *testing.T) {
	now := metav1.Now()
	obj := newTestObj("test", "default")
	obj.DeletionTimestamp = &now
	obj.Finalizers = []string{"test.io/sub1"}

	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	finalizeCalled := false
	sub := &finalizerSub{
		name: "sub1",
		processFn: func(context.Context, client.Object) (subroutines.Result, error) {
			t.Fatal("Process should not be called during finalization")
			return subroutines.OK(), nil
		},
		finalizeFn: func(context.Context, client.Object) (subroutines.Result, error) {
			finalizeCalled = true
			return subroutines.OK(), nil
		},
		finalizers: []string{"test.io/sub1"},
	}

	lc := setupLifecycle(cl, sub)

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assert.True(t, finalizeCalled)

	// Finalizer should have been removed — with deletionTimestamp set and no
	// finalizers remaining, the fake client deletes the object.
	fetched := &testObject{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched)
	assert.True(t, apierrors.IsNotFound(err), "object should be deleted after finalizer removal")
}

func TestFinalizeWithRequeue_KeepsFinalizer(t *testing.T) {
	now := metav1.Now()
	obj := newTestObj("test", "default")
	obj.DeletionTimestamp = &now
	obj.Finalizers = []string{"test.io/sub1"}

	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &finalizerSub{
		name: "sub1",
		processFn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.OK(), nil
		},
		finalizeFn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.Pending(5*time.Second, "still cleaning up"), nil
		},
		finalizers: []string{"test.io/sub1"},
	}

	lc := setupLifecycle(cl, sub)

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, result.RequeueAfter)

	// Finalizer should still be present.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	assert.Contains(t, fetched.Finalizers, "test.io/sub1")
}

func TestReadOnlyMode(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn: func(_ context.Context, o client.Object) (subroutines.Result, error) {
			// Modify labels — should NOT be patched in read-only mode.
			labels := o.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels["new-label"] = "value"
			o.SetLabels(labels)
			return subroutines.OK(), nil
		},
	}

	lc := setupLifecycle(cl, sub).WithReadOnly()

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)

	// Labels should NOT have been patched.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	assert.Empty(t, fetched.Labels)
}

func TestPrepareContext(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	type ctxKey struct{}
	prepareCalled := false

	sub := &processorSub{
		name: "step1",
		fn: func(ctx context.Context, _ client.Object) (subroutines.Result, error) {
			val := ctx.Value(ctxKey{})
			assert.Equal(t, "enriched", val)
			return subroutines.OK(), nil
		},
	}

	lc := setupLifecycle(cl, sub).
		WithPrepareContext(func(ctx context.Context, _ client.Object) (context.Context, error) {
			prepareCalled = true
			return context.WithValue(ctx, ctxKey{}, "enriched"), nil
		})

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.True(t, prepareCalled)
}

func TestNoConditionsConfigured(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn:   func(context.Context, client.Object) (subroutines.Result, error) { return subroutines.OK(), nil },
	}

	// No WithConditions call.
	lc := setupLifecycle(cl, sub)

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)

	// No conditions should be set.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	assert.Empty(t, fetched.Status.Conditions)
}

func TestErrorReporter_NotCalledOnStop(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.Stop("halted"), nil
		},
	}

	reporter := &fakeErrorReporter{}
	lc := setupLifecycle(cl, sub).WithErrorReporters(reporter)

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Empty(t, reporter.reported)
}

func TestAddFinalizers(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	processCalled := false
	sub := &finalizerSub{
		name: "sub1",
		processFn: func(context.Context, client.Object) (subroutines.Result, error) {
			processCalled = true
			return subroutines.OK(), nil
		},
		finalizeFn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.OK(), nil
		},
		finalizers: []string{"test.io/sub1"},
	}

	lc := setupLifecycle(cl, sub)

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)

	// Finalizer should be set.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	assert.Contains(t, fetched.Finalizers, "test.io/sub1")

	// Subroutines should not run on the first reconcile when finalizers are added.
	assert.False(t, processCalled, "Process should not be called when finalizers are being added")
}

func TestOKWithRequeue(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.OKWithRequeue(15 * time.Second), nil
		},
	}

	lc := setupLifecycle(cl, sub)

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, result.RequeueAfter)
}

func TestClientInContext(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn: func(ctx context.Context, _ client.Object) (subroutines.Result, error) {
			ctxClient, err := subroutines.ClientFromContext(ctx)
			require.NoError(t, err)
			assert.NotNil(t, ctxClient)
			return subroutines.OK(), nil
		},
	}

	lc := setupLifecycle(cl, sub)

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
}

func TestFinalizeOrder_Reversed(t *testing.T) {
	now := metav1.Now()
	obj := newTestObj("test", "default")
	obj.DeletionTimestamp = &now
	obj.Finalizers = []string{"test.io/sub1", "test.io/sub2"}

	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	callOrder := []string{}
	sub1 := &finalizerSub{
		name: "sub1",
		processFn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.OK(), nil
		},
		finalizeFn: func(context.Context, client.Object) (subroutines.Result, error) {
			callOrder = append(callOrder, "sub1")
			return subroutines.OK(), nil
		},
		finalizers: []string{"test.io/sub1"},
	}
	sub2 := &finalizerSub{
		name: "sub2",
		processFn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.OK(), nil
		},
		finalizeFn: func(context.Context, client.Object) (subroutines.Result, error) {
			callOrder = append(callOrder, "sub2")
			return subroutines.OK(), nil
		},
		finalizers: []string{"test.io/sub2"},
	}

	lc := setupLifecycle(cl, sub1, sub2)

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)

	// Should be reversed: sub2 first, then sub1.
	assert.Equal(t, []string{"sub2", "sub1"}, callOrder)
}

func TestNonFinalizerSub_SkippedDuringDeletion(t *testing.T) {
	now := metav1.Now()
	obj := newTestObj("test", "default")
	obj.DeletionTimestamp = &now
	obj.Finalizers = []string{"test.io/keep"} // fake client requires finalizer with deletionTimestamp

	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	called := false
	sub := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			called = true
			return subroutines.OK(), nil
		},
	}

	lc := setupLifecycle(cl, sub)

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.False(t, called, "processor should not be called during deletion")
}

// --- Initializer / Terminator Subroutine ---

type initializerSub struct {
	name         string
	processFn    func(context.Context, client.Object) (subroutines.Result, error)
	initializeFn func(context.Context, client.Object) (subroutines.Result, error)
}

func (s *initializerSub) GetName() string { return s.name }
func (s *initializerSub) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return s.processFn(ctx, obj)
}
func (s *initializerSub) Initialize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return s.initializeFn(ctx, obj)
}

type terminatorSub struct {
	name        string
	finalizeFn  func(context.Context, client.Object) (subroutines.Result, error)
	terminateFn func(context.Context, client.Object) (subroutines.Result, error)
	finalizers  []string
}

func (s *terminatorSub) GetName() string { return s.name }
func (s *terminatorSub) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return s.finalizeFn(ctx, obj)
}
func (s *terminatorSub) Finalizers(client.Object) []string { return s.finalizers }
func (s *terminatorSub) Terminate(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	return s.terminateFn(ctx, obj)
}

func TestInitializer_CalledWhenMarkerPresent(t *testing.T) {
	obj := newTestObj("test", "default")
	obj.Status.Initializers = []string{"bootstrap"}

	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	initCalled := false
	processCalled := false
	sub := &initializerSub{
		name: "step1",
		processFn: func(context.Context, client.Object) (subroutines.Result, error) {
			processCalled = true
			return subroutines.OK(), nil
		},
		initializeFn: func(context.Context, client.Object) (subroutines.Result, error) {
			initCalled = true
			return subroutines.OK(), nil
		},
	}

	lc := setupLifecycle(cl, sub).WithInitializer("bootstrap")

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.True(t, initCalled, "Initialize should be called when initializer marker is in status")
	assert.False(t, processCalled, "Process should not be called when Initialize is used")

	// Initializer should be removed from status after success.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	assert.Empty(t, fetched.Status.Initializers, "initializer should be removed from status")
}

func TestInitializer_ProcessCalledWhenNoMarker(t *testing.T) {
	obj := newTestObj("test", "default")
	// No initializer marker in status.

	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	initCalled := false
	processCalled := false
	sub := &initializerSub{
		name: "step1",
		processFn: func(context.Context, client.Object) (subroutines.Result, error) {
			processCalled = true
			return subroutines.OK(), nil
		},
		initializeFn: func(context.Context, client.Object) (subroutines.Result, error) {
			initCalled = true
			return subroutines.OK(), nil
		},
	}

	lc := setupLifecycle(cl, sub).WithInitializer("bootstrap")

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.False(t, initCalled, "Initialize should not be called without marker")
	assert.True(t, processCalled, "Process should be called when no initializer marker")
}

func TestTerminator_CalledWhenMarkerPresent(t *testing.T) {
	now := metav1.Now()
	obj := newTestObj("test", "default")
	obj.DeletionTimestamp = &now
	obj.Finalizers = []string{"test.io/sub1"}
	obj.Status.Terminators = []string{"teardown"}

	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	terminateCalled := false
	finalizeCalled := false
	sub := &terminatorSub{
		name: "sub1",
		finalizeFn: func(context.Context, client.Object) (subroutines.Result, error) {
			finalizeCalled = true
			return subroutines.OK(), nil
		},
		terminateFn: func(context.Context, client.Object) (subroutines.Result, error) {
			terminateCalled = true
			return subroutines.OK(), nil
		},
		finalizers: []string{"test.io/sub1"},
	}

	lc := setupLifecycle(cl, sub).WithTerminator("teardown")

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.True(t, terminateCalled, "Terminate should be called when terminator marker is in status")
	assert.False(t, finalizeCalled, "Finalize should not be called when Terminate is used")

	// Terminator should be removed from status after success.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	assert.Empty(t, fetched.Status.Terminators, "terminator should be removed from status")
}

func TestInitializer_NotRemovedOnPending(t *testing.T) {
	obj := newTestObj("test", "default")
	obj.Status.Initializers = []string{"bootstrap"}

	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &initializerSub{
		name: "step1",
		processFn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.OK(), nil
		},
		initializeFn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.Pending(5*time.Second, "not ready yet"), nil
		},
	}

	lc := setupLifecycle(cl, sub).WithInitializer("bootstrap")

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, result.RequeueAfter)

	// Initializer should still be in status.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	assert.Contains(t, fetched.Status.Initializers, "bootstrap", "initializer should be kept when pending")
}

// --- Fake Spread Manager ---

type fakeSpreadManager struct {
	reconcileRequired    bool
	requeueDelay         time.Duration
	observedGenUpdated   bool
	nextReconcileTimeSet bool
}

func (f *fakeSpreadManager) ReconcileRequired(client.Object) bool     { return f.reconcileRequired }
func (f *fakeSpreadManager) RequeueDelay(client.Object) time.Duration { return f.requeueDelay }
func (f *fakeSpreadManager) SetNextReconcileTime(client.Object)       { f.nextReconcileTimeSet = true }
func (f *fakeSpreadManager) UpdateObservedGeneration(client.Object)   { f.observedGenUpdated = true }
func (f *fakeSpreadManager) RemoveRefreshLabel(client.Object) bool    { return false }

func TestSpread_SkipsWhenNotDue(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	processCalled := false
	sub := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			processCalled = true
			return subroutines.OK(), nil
		},
	}

	sm := &fakeSpreadManager{reconcileRequired: false, requeueDelay: 30 * time.Minute}
	lc := setupLifecycle(cl, sub).WithSpread(sm)

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.False(t, processCalled, "subroutine should not run when spread says not due")
	assert.Equal(t, 30*time.Minute, result.RequeueAfter)
}

func TestSpread_ReconcileWhenDue(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	processCalled := false
	sub := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			processCalled = true
			return subroutines.OK(), nil
		},
	}

	sm := &fakeSpreadManager{reconcileRequired: true}
	lc := setupLifecycle(cl, sub).WithSpread(sm)

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.True(t, processCalled, "subroutine should run when spread says due")
}

func TestSpread_NotAdvancedOnStopWithRequeue(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.StopWithRequeue(30*time.Second, "rate limited"), nil
		},
	}

	sm := &fakeSpreadManager{reconcileRequired: true}
	lc := setupLifecycle(cl, sub).WithSpread(sm)

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, result.RequeueAfter)
	assert.False(t, sm.observedGenUpdated, "observed generation should not be updated when chain stopped with requeue")
	assert.False(t, sm.nextReconcileTimeSet, "next reconcile time should not be set when chain stopped with requeue")
}

func TestSpread_NotAdvancedOnPendingWithRequeue(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.Pending(10*time.Second, "waiting for external resource"), nil
		},
	}

	sm := &fakeSpreadManager{reconcileRequired: true}
	lc := setupLifecycle(cl, sub).WithSpread(sm)

	result, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, result.RequeueAfter)
	assert.False(t, sm.observedGenUpdated, "observed generation should not be updated when subroutine is pending")
	assert.False(t, sm.nextReconcileTimeSet, "next reconcile time should not be set when subroutine is pending")
}

func TestSpread_AdvancedOnSuccess(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			return subroutines.OK(), nil
		},
	}

	sm := &fakeSpreadManager{reconcileRequired: true}
	lc := setupLifecycle(cl, sub).WithSpread(sm)

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)
	assert.True(t, sm.observedGenUpdated, "observed generation should be updated on successful reconciliation")
	assert.True(t, sm.nextReconcileTimeSet, "next reconcile time should be set on successful reconciliation")
}

// --- incompatible object type ---

type plainObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

func (p *plainObject) DeepCopyObject() runtime.Object { cp := *p; return &cp }

func TestWithConditions_PanicsForIncompatibleObject(t *testing.T) {
	lc := New(nil, "test", func() client.Object { return &plainObject{} })
	assert.PanicsWithValue(t,
		`lifecycle "test": object type *lifecycle.plainObject does not implement conditions.ConditionAccessor`,
		func() { lc.WithConditions(conditions.NewManager()) },
	)
}

func TestWithSpread_PanicsForIncompatibleObject(t *testing.T) {
	lc := New(nil, "test", func() client.Object { return &plainObject{} })
	assert.PanicsWithValue(t,
		`lifecycle "test": object type *lifecycle.plainObject does not implement spread.SpreadReconcileStatus`,
		func() { lc.WithSpread(&fakeSpreadManager{}) },
	)
}

func TestSpecPatch_PatchesSpecChanges(t *testing.T) {
	obj := newTestObj("test", "default")
	obj.Spec.Value = "original"
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn: func(_ context.Context, o client.Object) (subroutines.Result, error) {
			o.(*testObject).Spec.Value = "modified"
			return subroutines.OK(), nil
		},
	}

	lc := setupLifecycle(cl, sub).WithSpecPatch()

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)

	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	assert.Equal(t, "modified", fetched.Spec.Value)
}

func TestNoSpecPatch_IgnoresSpecChanges(t *testing.T) {
	obj := newTestObj("test", "default")
	obj.Spec.Value = "original"
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	sub := &processorSub{
		name: "step1",
		fn: func(_ context.Context, o client.Object) (subroutines.Result, error) {
			o.(*testObject).Spec.Value = "modified"
			return subroutines.OK(), nil
		},
	}

	// No WithSpecPatch — spec changes should not be persisted.
	lc := setupLifecycle(cl, sub)

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.NoError(t, err)

	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, fetched))
	assert.Equal(t, "original", fetched.Spec.Value)
}

func TestPrepareContext_Error(t *testing.T) {
	obj := newTestObj("test", "default")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(obj).WithStatusSubresource(obj).Build()

	processCalled := false
	sub := &processorSub{
		name: "step1",
		fn: func(context.Context, client.Object) (subroutines.Result, error) {
			processCalled = true
			return subroutines.OK(), nil
		},
	}

	lc := setupLifecycle(cl, sub).
		WithPrepareContext(func(context.Context, client.Object) (context.Context, error) {
			return nil, errors.New("context setup failed")
		})

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "preparing context")
	assert.False(t, processCalled, "subroutines should not run when prepareContext fails")
}

func TestClusterFromContext_Error(t *testing.T) {
	mgr := &fakeManagerWithError{err: errors.New("cluster not found")}
	lc := New(mgr, "test-controller", func() client.Object { return &testObject{} })

	_, err := lc.Reconcile(context.Background(), newReq("test", "default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving cluster client")
}
