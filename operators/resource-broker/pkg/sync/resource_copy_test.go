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

package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestStripClusterMetadata(t *testing.T) {
	t.Parallel()

	t.Run("removes cluster-specific metadata fields", func(t *testing.T) {
		t.Parallel()

		obj := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]any{
					"name":              "test-cm",
					"namespace":         "test-ns",
					"resourceVersion":   "12345",
					"uid":               "abc-123",
					"creationTimestamp": "2024-01-01T00:00:00Z",
					"managedFields":     []any{},
					"generation":        int64(1),
					"ownerReferences":   []any{},
					"finalizers":        []any{"test-finalizer"},
					"annotations": map[string]any{
						"test": "annotation",
					},
					"labels": map[string]any{
						"test": "label",
					},
				},
				"spec": map[string]any{
					"data": map[string]any{
						"key": "value",
					},
				},
				"status": map[string]any{
					"phase": "Running",
				},
			},
		}

		stripped := StripClusterMetadata(obj)

		// Check that status is removed
		_, hasStatus := stripped.Object["status"]
		assert.False(t, hasStatus, "status should be removed")

		// Check metadata fields are removed
		metadata, ok := stripped.Object["metadata"].(map[string]any)
		require.True(t, ok, "metadata should exist")

		assert.NotContains(t, metadata, "resourceVersion")
		assert.NotContains(t, metadata, "uid")
		assert.NotContains(t, metadata, "creationTimestamp")
		assert.NotContains(t, metadata, "managedFields")
		assert.NotContains(t, metadata, "generation")
		assert.NotContains(t, metadata, "ownerReferences")
		assert.NotContains(t, metadata, "finalizers")
		assert.NotContains(t, metadata, "annotations")
		assert.NotContains(t, metadata, "labels")

		// Check that name and namespace are preserved
		assert.Equal(t, "test-cm", metadata["name"])
		assert.Equal(t, "test-ns", metadata["namespace"])

		// Check that spec is preserved
		spec, hasSpec := stripped.Object["spec"]
		assert.True(t, hasSpec, "spec should be preserved")
		assert.NotNil(t, spec)
	})

	t.Run("does not modify original object", func(t *testing.T) {
		t.Parallel()

		obj := &unstructured.Unstructured{
			Object: map[string]any{
				"metadata": map[string]any{
					"name":            "test",
					"resourceVersion": "123",
				},
				"status": map[string]any{
					"phase": "Running",
				},
			},
		}

		stripped := StripClusterMetadata(obj)

		// Original should still have status
		_, hasStatus := obj.Object["status"]
		assert.True(t, hasStatus, "original object should still have status")

		// Stripped should not have status
		_, hasStatus = stripped.Object["status"]
		assert.False(t, hasStatus, "stripped object should not have status")
	})

	t.Run("handles object without metadata", func(t *testing.T) {
		t.Parallel()

		obj := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
			},
		}

		stripped := StripClusterMetadata(obj)
		assert.NotNil(t, stripped)
	})
}

func TestEqualObjects(t *testing.T) {
	t.Parallel()

	t.Run("equal objects", func(t *testing.T) {
		t.Parallel()

		a := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]any{
					"name": "test-cm",
				},
				"spec": map[string]any{
					"data": map[string]any{
						"key": "value",
					},
				},
			},
		}
		b := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]any{
					"name": "test-cm",
				},
				"spec": map[string]any{
					"data": map[string]any{
						"key": "value",
					},
				},
			},
		}

		assert.True(t, EqualObjects(a, b))
	})

	t.Run("different objects", func(t *testing.T) {
		t.Parallel()

		a := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"spec": map[string]any{
					"data": map[string]any{
						"key": "value1",
					},
				},
			},
		}
		b := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"spec": map[string]any{
					"data": map[string]any{
						"key": "value2",
					},
				},
			},
		}

		assert.False(t, EqualObjects(a, b))
	})

	t.Run("ignores metadata differences", func(t *testing.T) {
		t.Parallel()

		a := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]any{
					"name":            "test-cm",
					"resourceVersion": "123",
					"uid":             "abc-123",
					"labels": map[string]any{
						"app": "test",
					},
				},
				"spec": map[string]any{
					"data": map[string]any{
						"key": "value",
					},
				},
			},
		}
		b := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]any{
					"name":            "test-cm",
					"resourceVersion": "456",
					"uid":             "xyz-789",
					"labels": map[string]any{
						"app": "different",
					},
				},
				"spec": map[string]any{
					"data": map[string]any{
						"key": "value",
					},
				},
			},
		}

		assert.True(t, EqualObjects(a, b))
	})

	t.Run("ignores status differences", func(t *testing.T) {
		t.Parallel()

		a := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Pod",
				"spec": map[string]any{
					"containers": []any{},
				},
				"status": map[string]any{
					"phase": "Running",
				},
			},
		}
		b := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Pod",
				"spec": map[string]any{
					"containers": []any{},
				},
				"status": map[string]any{
					"phase": "Pending",
				},
			},
		}

		assert.True(t, EqualObjects(a, b))
	})

	t.Run("detects field presence difference", func(t *testing.T) {
		t.Parallel()

		a := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"spec": map[string]any{
					"replicas": int64(3),
				},
			},
		}
		b := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"spec": map[string]any{
					"replicas": int64(5),
				},
			},
		}

		assert.False(t, EqualObjects(a, b))
	})
}

func TestMakeCond(t *testing.T) {
	t.Parallel()

	t.Run("creates condition with true status", func(t *testing.T) {
		t.Parallel()

		cond := makeCond(ConditionResourceCopied, true, "Success", "Resource copied successfully")

		assert.Equal(t, "Copied", cond.Type)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, "Success", cond.Reason)
		assert.Equal(t, "Resource copied successfully", cond.Message)
		assert.False(t, cond.LastTransitionTime.IsZero())
	})

	t.Run("creates condition with false status", func(t *testing.T) {
		t.Parallel()

		cond := makeCond(ConditionStatusSynced, false, "Failed", "Status sync failed")

		assert.Equal(t, "StatusSynced", cond.Type)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, "Failed", cond.Reason)
		assert.Equal(t, "Status sync failed", cond.Message)
		assert.False(t, cond.LastTransitionTime.IsZero())
	})
}
