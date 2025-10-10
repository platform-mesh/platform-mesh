package subroutine_test

import (
	"context"
	"testing"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	"github.com/platform-mesh/security-operator/internal/subroutine/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// fakeStatusWriter implements client.SubResourceWriter to intercept Status().Patch calls
type fakeStatusWriter struct {
	t           *testing.T
	expectClear kcpv1alpha1.LogicalClusterInitializer
	err         error
}

func (f *fakeStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}

func (f *fakeStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return nil
}

func (f *fakeStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	lc := obj.(*kcpv1alpha1.LogicalCluster)
	// Ensure initializer was removed before patch
	for _, init := range lc.Status.Initializers {
		if init == f.expectClear {
			f.t.Fatalf("initializer %q should have been removed prior to Patch", string(init))
		}
	}
	return f.err
}

func TestRemoveInitializer_Process(t *testing.T) {
	const initializerName = "foo.initializer.kcp.dev"

	t.Run("skips when initializer is absent", func(t *testing.T) {
		mgr := mocks.NewMockManager(t)
		cluster := mocks.NewMockCluster(t)
		mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)

		lc := &kcpv1alpha1.LogicalCluster{}
		lc.Status.Initializers = []kcpv1alpha1.LogicalClusterInitializer{"other.initializer"}

		r := subroutine.NewRemoveInitializer(mgr, initializerName)
		_, err := r.Process(context.Background(), lc)
		assert.Nil(t, err)
	})

	t.Run("removes initializer and patches status", func(t *testing.T) {
		mgr := mocks.NewMockManager(t)
		cluster := mocks.NewMockCluster(t)
		k8s := mocks.NewMockClient(t)

		mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
		cluster.EXPECT().GetClient().Return(k8s)
		k8s.EXPECT().Status().Return(&fakeStatusWriter{t: t, expectClear: kcpv1alpha1.LogicalClusterInitializer(initializerName), err: nil})

		lc := &kcpv1alpha1.LogicalCluster{}
		lc.Status.Initializers = []kcpv1alpha1.LogicalClusterInitializer{
			kcpv1alpha1.LogicalClusterInitializer(initializerName),
			"another.initializer",
		}

		r := subroutine.NewRemoveInitializer(mgr, initializerName)
		_, err := r.Process(context.Background(), lc)
		assert.Nil(t, err)
		// ensure it's removed in in-memory object as well
		for _, init := range lc.Status.Initializers {
			assert.NotEqual(t, initializerName, string(init))
		}
	})

	t.Run("returns error when status patch fails", func(t *testing.T) {
		mgr := mocks.NewMockManager(t)
		cluster := mocks.NewMockCluster(t)
		k8s := mocks.NewMockClient(t)

		mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
		cluster.EXPECT().GetClient().Return(k8s)
		k8s.EXPECT().Status().Return(&fakeStatusWriter{t: t, expectClear: kcpv1alpha1.LogicalClusterInitializer(initializerName), err: assert.AnError})

		lc := &kcpv1alpha1.LogicalCluster{}
		lc.Status.Initializers = []kcpv1alpha1.LogicalClusterInitializer{
			kcpv1alpha1.LogicalClusterInitializer(initializerName),
		}

		r := subroutine.NewRemoveInitializer(mgr, initializerName)
		_, err := r.Process(context.Background(), lc)
		assert.NotNil(t, err)
	})
}

func TestRemoveInitializer_Misc(t *testing.T) {
	mgr := mocks.NewMockManager(t)
	r := subroutine.NewRemoveInitializer(mgr, "foo.initializer.kcp.dev")

	assert.Equal(t, "RemoveInitializer", r.GetName())
	assert.Equal(t, []string{}, r.Finalizers(nil))

	_, err := r.Finalize(context.Background(), &kcpv1alpha1.LogicalCluster{})
	assert.Nil(t, err)
}

func TestRemoveInitializer_ManagerError(t *testing.T) {
	mgr := mocks.NewMockManager(t)
	// Simulate error fetching cluster from context
	mgr.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError)

	r := subroutine.NewRemoveInitializer(mgr, "foo.initializer.kcp.dev")
	_, err := r.Process(context.Background(), &kcpv1alpha1.LogicalCluster{})
	assert.NotNil(t, err)
}
