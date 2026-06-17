package predicates

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func logicalClusterWithInitializers(spec, status []string) *kcpcorev1alpha1.LogicalCluster {
	lc := &kcpcorev1alpha1.LogicalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
	}
	for _, i := range spec {
		lc.Spec.Initializers = append(lc.Spec.Initializers, kcpcorev1alpha1.LogicalClusterInitializer(i))
	}
	for _, i := range status {
		lc.Status.Initializers = append(lc.Status.Initializers, kcpcorev1alpha1.LogicalClusterInitializer(i))
	}
	return lc
}

func TestHasInitializerPredicate(t *testing.T) {
	const initName = "platform-mesh.io/my-init"
	pred := HasInitializerPredicate(initName)

	tests := []struct {
		name     string
		obj      client.Object
		expected bool
	}{
		{
			name:     "in spec only: should reconcile",
			obj:      logicalClusterWithInitializers([]string{initName}, nil),
			expected: true,
		},
		{
			name:     "in spec and status: already initialised, skip",
			obj:      logicalClusterWithInitializers([]string{initName}, []string{initName}),
			expected: false,
		},
		{
			name:     "not in spec: skip",
			obj:      logicalClusterWithInitializers(nil, nil),
			expected: false,
		},
		{
			name:     "in status only: skip",
			obj:      logicalClusterWithInitializers(nil, []string{initName}),
			expected: false,
		},
		{
			name:     "different initializer in spec: skip",
			obj:      logicalClusterWithInitializers([]string{"other-init"}, nil),
			expected: false,
		},
		{
			name:     "non-LogicalCluster returns false",
			obj:      &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod"}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, pred.Create(event.CreateEvent{Object: tt.obj}))
			assert.Equal(t, tt.expected, pred.Update(event.UpdateEvent{ObjectNew: tt.obj}))
			assert.Equal(t, tt.expected, pred.Delete(event.DeleteEvent{Object: tt.obj}))
			assert.Equal(t, tt.expected, pred.Generic(event.GenericEvent{Object: tt.obj}))
		})
	}
}
