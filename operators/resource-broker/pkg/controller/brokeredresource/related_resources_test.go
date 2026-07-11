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

package brokeredresource

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const testRelatedName = "my-widget-credentials"

// testStagingCopyWithRelated returns a staging copy publishing a related
// config map in its status.
func testStagingCopyWithRelated(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	obj := testStagingCopy()
	require.NoError(t, unstructured.SetNestedMap(obj.Object, map[string]any{
		"credentials": map[string]any{
			"namespace": testNamespace,
			"name":      testRelatedName,
			"gvk": map[string]any{
				"group":   "core",
				"version": "v1",
				"kind":    "ConfigMap",
			},
		},
	}, "status", "relatedResources"), "setting related resources")
	return obj
}

func testRelatedConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRelatedName,
		},
		Data: map[string]string{"user": "widget-user"},
	}
}

func TestRelatedResourcesGetName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "RelatedResources", (&relatedResourcesSubroutine{}).GetName())
}

func TestRelatedResourcesFinalizers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{RelatedResourcesFinalizer}, (&relatedResourcesSubroutine{}).Finalizers(nil))
}

func TestRelatedResourcesProcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		assignments []ctrlruntimeclient.Object
		stagingObjs []ctrlruntimeclient.Object
		wantMsg     string
		wantCopied  bool
	}{
		{
			name:    "waits for assignment",
			wantMsg: "waiting for assignment",
		},
		{
			name: "waits for staging copy",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			wantMsg: "waiting for staging copy",
		},
		{
			name: "no related resources",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			stagingObjs: []ctrlruntimeclient.Object{testStagingCopy()},
		},
		{
			name: "copies related resources",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			stagingObjs: func() []ctrlruntimeclient.Object {
				return []ctrlruntimeclient.Object{
					testStagingCopyWithRelated(t),
					testRelatedConfigMap(),
				}
			}(),
			wantCopied: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			consumer := testFakeClient(t, testConsumerObject())
			clients := testClients{
				coordination: testFakeClient(t, tc.assignments...),
				staging:      testFakeClient(t, tc.stagingObjs...),
			}
			opts := testOptions(t, clients, nil)
			opts.RequeueInterval = DefaultRequeueInterval
			s := &relatedResourcesSubroutine{opts: opts}

			ctx := testCopyContext(t, consumer)
			result, err := s.Process(ctx, testConsumerObject())
			require.NoError(t, err)

			if tc.wantMsg != "" {
				assert.Equal(t, tc.wantMsg, result.Message())
				assert.Positive(t, result.Requeue())
			} else {
				assert.True(t, result.IsContinue())
			}

			cm := &corev1.ConfigMap{}
			err = consumer.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: testRelatedName}, cm)
			if !tc.wantCopied {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "widget-user", cm.Data["user"])
		})
	}
}

func TestRelatedResourcesProcessUpdatesDrift(t *testing.T) {
	t.Parallel()

	drifted := testRelatedConfigMap()
	drifted.Data = map[string]string{"user": "stale-user"}

	consumer := testFakeClient(t, testConsumerObject(), drifted)
	clients := testClients{
		coordination: testFakeClient(t, testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound)),
		staging:      testFakeClient(t, testStagingCopyWithRelated(t), testRelatedConfigMap()),
	}
	opts := testOptions(t, clients, nil)
	opts.RequeueInterval = DefaultRequeueInterval
	s := &relatedResourcesSubroutine{opts: opts}

	ctx := testCopyContext(t, consumer)
	_, err := s.Process(ctx, testConsumerObject())
	require.NoError(t, err)

	cm := &corev1.ConfigMap{}
	require.NoError(t, consumer.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: testRelatedName}, cm))
	assert.Equal(t, "widget-user", cm.Data["user"])
}

func TestRelatedResourcesFinalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		assignments  []ctrlruntimeclient.Object
		stagingObjs  []ctrlruntimeclient.Object
		consumerObjs []ctrlruntimeclient.Object
		wantOK       bool
		wantGone     bool
	}{
		{
			name:     "no assignment",
			wantOK:   true,
			wantGone: true,
		},
		{
			name: "staging copy gone",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			wantOK:   true,
			wantGone: true,
		},
		{
			name: "related resource already gone",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			stagingObjs: func() []ctrlruntimeclient.Object {
				return []ctrlruntimeclient.Object{testStagingCopyWithRelated(t)}
			}(),
			wantOK:   true,
			wantGone: true,
		},
		{
			name: "deletes related resources",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			stagingObjs: func() []ctrlruntimeclient.Object {
				return []ctrlruntimeclient.Object{testStagingCopyWithRelated(t)}
			}(),
			consumerObjs: []ctrlruntimeclient.Object{testRelatedConfigMap()},
			wantGone:     true,
		},
		{
			name: "waits for terminating related resource",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			stagingObjs: func() []ctrlruntimeclient.Object {
				return []ctrlruntimeclient.Object{testStagingCopyWithRelated(t)}
			}(),
			consumerObjs: []ctrlruntimeclient.Object{
				func() ctrlruntimeclient.Object {
					cm := testRelatedConfigMap()
					cm.DeletionTimestamp = &metav1.Time{Time: time.Now()}
					cm.Finalizers = []string{"keep/me"}
					return cm
				}(),
			},
			wantGone: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			consumer := testFakeClient(t, tc.consumerObjs...)
			clients := testClients{
				coordination: testFakeClient(t, tc.assignments...),
				staging:      testFakeClient(t, tc.stagingObjs...),
			}
			opts := testOptions(t, clients, nil)
			opts.RequeueInterval = DefaultRequeueInterval
			s := &relatedResourcesSubroutine{opts: opts}

			ctx := testCopyContext(t, consumer)
			result, err := s.Finalize(ctx, testConsumerObject())
			require.NoError(t, err)

			if tc.wantOK {
				assert.True(t, result.IsContinue())
				assert.Zero(t, result.Requeue())
			} else {
				assert.False(t, result.IsContinue())
				assert.Positive(t, result.Requeue())
			}

			cm := &corev1.ConfigMap{}
			err = consumer.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: testRelatedName}, cm)
			if tc.wantGone {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
