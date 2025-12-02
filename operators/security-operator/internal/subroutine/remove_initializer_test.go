package subroutine_test

import (
	"context"
	"testing"
	"time"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/config"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	"github.com/platform-mesh/security-operator/internal/subroutine/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
		runtimeClient := mocks.NewMockClient(t)
		cluster := mocks.NewMockCluster(t)
		mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)

		lc := &kcpv1alpha1.LogicalCluster{}
		lc.Status.Initializers = []kcpv1alpha1.LogicalClusterInitializer{"other.initializer"}

		r := subroutine.NewRemoveInitializer(mgr, config.Config{InitializerName: initializerName, SecretWaitingTimeoutInSeconds: 60}, runtimeClient)
		_, err := r.Process(context.Background(), lc)
		assert.Nil(t, err)
	})

	t.Run("removes initializer and patches status", func(t *testing.T) {
		mgr := mocks.NewMockManager(t)
		cluster := mocks.NewMockCluster(t)
		runtimeClient := mocks.NewMockClient(t)
		k8s := mocks.NewMockClient(t)

		mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
		// Secret must exist for the flow to proceed
		runtimeClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "portal-client-secret-test", Namespace: subroutine.PortalClientSecretNamespace}, mock.Anything).Return(nil)
		cluster.EXPECT().GetClient().Return(k8s)
		k8s.EXPECT().Status().Return(&fakeStatusWriter{t: t, expectClear: kcpv1alpha1.LogicalClusterInitializer(initializerName), err: nil})

		lc := &kcpv1alpha1.LogicalCluster{}
		lc.Status.Initializers = []kcpv1alpha1.LogicalClusterInitializer{
			kcpv1alpha1.LogicalClusterInitializer(initializerName),
			"another.initializer",
		}
		lc.Annotations = map[string]string{"kcp.io/path": "root:org:test"}

		r := subroutine.NewRemoveInitializer(mgr, config.Config{InitializerName: initializerName, SecretWaitingTimeoutInSeconds: 60}, runtimeClient)
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
		runtimeClient := mocks.NewMockClient(t)
		k8s := mocks.NewMockClient(t)

		mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
		// Secret exists so we hit the patch failure path
		runtimeClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "portal-client-secret-test", Namespace: subroutine.PortalClientSecretNamespace}, mock.Anything).Return(nil)
		cluster.EXPECT().GetClient().Return(k8s)
		k8s.EXPECT().Status().Return(&fakeStatusWriter{t: t, expectClear: kcpv1alpha1.LogicalClusterInitializer(initializerName), err: assert.AnError})

		lc := &kcpv1alpha1.LogicalCluster{}
		lc.Status.Initializers = []kcpv1alpha1.LogicalClusterInitializer{
			kcpv1alpha1.LogicalClusterInitializer(initializerName),
		}
		lc.Annotations = map[string]string{"kcp.io/path": "root:org:test"}

		r := subroutine.NewRemoveInitializer(mgr, config.Config{InitializerName: initializerName, SecretWaitingTimeoutInSeconds: 60}, runtimeClient)
		_, err := r.Process(context.Background(), lc)
		assert.NotNil(t, err)
	})

	t.Run("requeues when secret not found under 1 minute", func(t *testing.T) {
		mgr := mocks.NewMockManager(t)
		cluster := mocks.NewMockCluster(t)
		runtimeClient := mocks.NewMockClient(t)

		mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
		// Simulate NotFound error
		runtimeClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "portal-client-secret-test", Namespace: subroutine.PortalClientSecretNamespace}, mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "secrets"}, "portal-client-secret-test"))

		lc := &kcpv1alpha1.LogicalCluster{}
		lc.Status.Initializers = []kcpv1alpha1.LogicalClusterInitializer{
			kcpv1alpha1.LogicalClusterInitializer(initializerName),
		}
		lc.Annotations = map[string]string{"kcp.io/path": "root:org:test"}
		lc.CreationTimestamp.Time = time.Now().Add(-30 * time.Second)

		r := subroutine.NewRemoveInitializer(mgr, config.Config{InitializerName: initializerName, SecretWaitingTimeoutInSeconds: 60}, runtimeClient)
		res, err := r.Process(context.Background(), lc)
		assert.Nil(t, err)
		assert.Equal(t, 5*time.Second, res.RequeueAfter)
	})

	t.Run("errors when secret not found after 1 minute", func(t *testing.T) {
		mgr := mocks.NewMockManager(t)
		cluster := mocks.NewMockCluster(t)
		runtimeClient := mocks.NewMockClient(t)

		mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
		// Simulate NotFound error
		runtimeClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "portal-client-secret-test", Namespace: subroutine.PortalClientSecretNamespace}, mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "secrets"}, "portal-client-secret-test"))

		lc := &kcpv1alpha1.LogicalCluster{}
		lc.Status.Initializers = []kcpv1alpha1.LogicalClusterInitializer{
			kcpv1alpha1.LogicalClusterInitializer(initializerName),
		}
		lc.Annotations = map[string]string{"kcp.io/path": "root:org:test"}
		lc.CreationTimestamp.Time = time.Now().Add(-2 * time.Minute)

		r := subroutine.NewRemoveInitializer(mgr, config.Config{InitializerName: initializerName, SecretWaitingTimeoutInSeconds: 60}, runtimeClient)
		_, err := r.Process(context.Background(), lc)
		assert.NotNil(t, err)
	})
}

func TestRemoveInitializer_Misc(t *testing.T) {
	mgr := mocks.NewMockManager(t)
	runtimeClient := mocks.NewMockClient(t)
	r := subroutine.NewRemoveInitializer(mgr, config.Config{InitializerName: "foo.initializer.kcp.dev", SecretWaitingTimeoutInSeconds: 60}, runtimeClient)

	assert.Equal(t, "RemoveInitializer", r.GetName())
	assert.Equal(t, []string{}, r.Finalizers(nil))

	_, err := r.Finalize(context.Background(), &kcpv1alpha1.LogicalCluster{})
	assert.Nil(t, err)
}

func TestRemoveInitializer_ManagerError(t *testing.T) {
	mgr := mocks.NewMockManager(t)
	// Simulate error fetching cluster from context
	mgr.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError)
	runtimeClient := mocks.NewMockClient(t)

	r := subroutine.NewRemoveInitializer(mgr, config.Config{InitializerName: "foo.initializer.kcp.dev", SecretWaitingTimeoutInSeconds: 60}, runtimeClient)
	_, err := r.Process(context.Background(), &kcpv1alpha1.LogicalCluster{})
	assert.NotNil(t, err)
}
