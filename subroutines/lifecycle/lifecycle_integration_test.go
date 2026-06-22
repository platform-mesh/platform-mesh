//go:build integration

package lifecycle

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/spread"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

// Integration test CRD (reuses testObject from unit tests).

func TestIntegration_FullLifecycle(t *testing.T) {
	// Start envtest.
	env := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "testdata", "crds")},
	}

	cfg, err := env.Start()
	require.NoError(t, err)
	defer env.Stop()

	s := runtime.NewScheme()
	require.NoError(t, metav1.AddToGroupVersion(s, schema.GroupVersion{Group: "test.io", Version: "v1alpha1"}))
	s.AddKnownTypeWithName(testGVK, &testObject{})

	cl, err := client.New(cfg, client.Options{Scheme: s})
	require.NoError(t, err)

	mgr := &fakeManager{cl: cl}
	condMgr := conditions.NewManager()
	spreadMgr := spread.NewManager(
		spread.WithMinDuration(1*time.Hour),
		spread.WithMaxDuration(2*time.Hour),
	)

	processCalled := false
	finalizeCalled := false
	sub := &finalizerSub{
		name: "integration-sub",
		processFn: func(ctx context.Context, obj client.Object) (subroutines.Result, error) {
			processCalled = true
			return subroutines.OK(), nil
		},
		finalizeFn: func(ctx context.Context, obj client.Object) (subroutines.Result, error) {
			finalizeCalled = true
			return subroutines.OK(), nil
		},
		finalizers: []string{"test.io/integration"},
	}

	lc := New(mgr, "integration-controller", func() client.Object { return &testObject{} }, sub).
		WithConditions(condMgr).
		WithSpread(spreadMgr)

	// Create test object.
	obj := newTestObj("integration-test", "default")
	require.NoError(t, cl.Create(context.Background(), obj))

	// First reconcile — should process, add finalizer, set conditions.
	result, err := lc.Reconcile(context.Background(), mcreconcile.Request{
		Request: reconcile.Request{NamespacedName: types.NamespacedName{Name: "integration-test", Namespace: "default"}},
	})
	require.NoError(t, err)
	assert.True(t, processCalled)
	assert.True(t, result.RequeueAfter > 0, "spread should set a requeue")

	// Verify finalizer.
	fetched := &testObject{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "integration-test", Namespace: "default"}, fetched))
	assert.Contains(t, fetched.Finalizers, "test.io/integration")

	// Verify Ready condition.
	readyCond := meta.FindStatusCondition(fetched.Status.Conditions, "Ready")
	require.NotNil(t, readyCond)
	assert.Equal(t, metav1.ConditionTrue, readyCond.Status)

	// Delete the object.
	require.NoError(t, cl.Delete(context.Background(), fetched))

	// Reconcile deletion.
	result, err = lc.Reconcile(context.Background(), mcreconcile.Request{
		Request: reconcile.Request{NamespacedName: types.NamespacedName{Name: "integration-test", Namespace: "default"}},
	})
	require.NoError(t, err)
	assert.True(t, finalizeCalled)

	// Object should be gone (finalizer removed, k8s deletes it).
	err = cl.Get(context.Background(), types.NamespacedName{Name: "integration-test", Namespace: "default"}, fetched)
	assert.True(t, apierrors.IsNotFound(err), "object should be deleted after finalizer removal")
}
