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

package predicates

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/event"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func logicalClusterWithPath(path string) *kcpcorev1alpha1.LogicalCluster {
	lc := &kcpcorev1alpha1.LogicalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
	}
	if path != "" {
		lc.Annotations = map[string]string{kcpPathAnnotation: path}
	}
	return lc
}

func TestLogicalClusterIsAccountTypeOrg(t *testing.T) {
	pred := LogicalClusterIsAccountTypeOrg()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "returns true for org path",
			path:     "root:orgs:myorg",
			expected: true,
		},
		{
			name:     "returns false for account path (4 parts)",
			path:     "root:orgs:myorg:myaccount",
			expected: false,
		},
		{
			name:     "returns false for too-short path",
			path:     "root:orgs",
			expected: false,
		},
		{
			name:     "returns false when path prefix is not root:orgs",
			path:     "root:other:myorg",
			expected: false,
		},
		{
			name:     "returns false when annotation is absent",
			path:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := logicalClusterWithPath(tt.path)
			assert.Equal(t, tt.expected, pred.Create(event.CreateEvent{Object: lc}))
			assert.Equal(t, tt.expected, pred.Update(event.UpdateEvent{ObjectNew: lc}))
			assert.Equal(t, tt.expected, pred.Delete(event.DeleteEvent{Object: lc}))
			assert.Equal(t, tt.expected, pred.Generic(event.GenericEvent{Object: lc}))
		})
	}

	t.Run("panics for non-LogicalCluster object", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod"}}
		assert.Panics(t, func() {
			pred.Create(event.CreateEvent{Object: pod})
		})
	})
}
