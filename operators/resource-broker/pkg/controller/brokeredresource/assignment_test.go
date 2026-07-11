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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

func TestAssignmentGetName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Assignment", (&assignmentSubroutine{}).GetName())
}

func TestAssignmentFinalizers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{AssignmentFinalizer}, (&assignmentSubroutine{}).Finalizers(nil))
}

func TestAssignmentProcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		refs           []AcceptAPIRef
		assignments    []ctrlruntimeclient.Object
		pick           func([]AcceptAPIRef) AcceptAPIRef
		wantErr        string
		wantOK         bool
		wantMsg        string
		wantAssignment bool
	}{
		{
			name:           "creates assignment for matching accept api",
			refs:           []AcceptAPIRef{{Cluster: testProviderCluster, AcceptAPI: testAcceptAPI()}},
			wantMsg:        "created assignment",
			wantAssignment: true,
		},
		{
			name:    "no matching accept api",
			refs:    nil,
			wantMsg: "no matching AcceptAPI",
		},
		{
			name: "non-matching gvr filtered out",
			refs: func() []AcceptAPIRef {
				a := testAcceptAPI()
				a.Spec.GVR.Resource = "gadgets"
				return []AcceptAPIRef{{Cluster: testProviderCluster, AcceptAPI: a}}
			}(),
			wantMsg: "no matching AcceptAPI",
		},
		{
			name: "waits for assignment to be bound",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhasePending),
			},
			wantMsg: "waiting for assignment to be bound",
		},
		{
			name: "terminating assignment",
			assignments: []ctrlruntimeclient.Object{
				func() ctrlruntimeclient.Object {
					a := testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound)
					a.DeletionTimestamp = &metav1.Time{Time: time.Now()}
					a.Finalizers = []string{"keep/me"}
					return a
				}(),
			},
			wantMsg: "assignment is terminating",
		},
		{
			name: "bound assignment",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			wantOK: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := testClients{
				coordination: testFakeClient(t, tc.assignments...),
				staging:      testFakeClient(t),
			}
			opts := testOptions(t, clients, tc.refs)
			opts.RequeueInterval = DefaultRequeueInterval
			if tc.pick != nil {
				opts.PickAcceptAPI = tc.pick
			} else {
				opts.PickAcceptAPI = func(refs []AcceptAPIRef) AcceptAPIRef { return refs[0] }
			}
			s := &assignmentSubroutine{opts: opts}

			ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)
			result, err := s.Process(ctx, testConsumerObject())

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)

			if tc.wantOK {
				assert.True(t, result.IsContinue())
				assert.Zero(t, result.Requeue())
			} else {
				assert.Equal(t, tc.wantMsg, result.Message())
				assert.Positive(t, result.Requeue())
			}

			assignment := &pmcoordbrokerv1alpha1.Assignment{}
			err = clients.coordination.Get(ctx, ctrlruntimeclient.ObjectKey{Name: testAssignmentName()}, assignment)
			if tc.wantAssignment {
				require.NoError(t, err)
				assert.Equal(t, testConsumerCluster, assignment.Spec.ConsumerCluster)
				assert.Equal(t, testGVR, assignment.Spec.GVR)
				assert.Equal(t, testNamespace, assignment.Spec.Namespace)
				assert.Equal(t, testResourceName, assignment.Spec.Name)
				assert.Equal(t, testProviderCluster, assignment.Spec.ProviderCluster)
				assert.Equal(t, testAcceptAPIName, assignment.Spec.AcceptAPIName)
			}
		})
	}
}

func TestAssignmentProcessPick(t *testing.T) {
	t.Parallel()

	second := testAcceptAPI()
	second.Name = "accept-widgets-2"
	refs := []AcceptAPIRef{
		{Cluster: testProviderCluster, AcceptAPI: testAcceptAPI()},
		{Cluster: "other-provider", AcceptAPI: second},
	}

	clients := testClients{
		coordination: testFakeClient(t),
		staging:      testFakeClient(t),
	}
	opts := testOptions(t, clients, refs)
	opts.RequeueInterval = DefaultRequeueInterval
	opts.PickAcceptAPI = func(refs []AcceptAPIRef) AcceptAPIRef { return refs[1] }
	s := &assignmentSubroutine{opts: opts}

	ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)
	_, err := s.Process(ctx, testConsumerObject())
	require.NoError(t, err)

	assignment := &pmcoordbrokerv1alpha1.Assignment{}
	require.NoError(t, clients.coordination.Get(ctx, ctrlruntimeclient.ObjectKey{Name: testAssignmentName()}, assignment))
	assert.Equal(t, "other-provider", assignment.Spec.ProviderCluster)
	assert.Equal(t, "accept-widgets-2", assignment.Spec.AcceptAPIName)
}

func TestAssignmentProcessNoClusterInContext(t *testing.T) {
	t.Parallel()

	s := &assignmentSubroutine{}
	_, err := s.Process(t.Context(), testConsumerObject())
	require.ErrorContains(t, err, "no cluster name in context")
}

func TestAssignmentFinalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		assignments []ctrlruntimeclient.Object
		wantOK      bool
		wantDeleted bool
	}{
		{
			name:        "assignment gone",
			wantOK:      true,
			wantDeleted: true,
		},
		{
			name: "deletes assignment",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			wantDeleted: true,
		},
		{
			name: "waits for terminating assignment",
			assignments: []ctrlruntimeclient.Object{
				func() ctrlruntimeclient.Object {
					a := testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound)
					a.DeletionTimestamp = &metav1.Time{Time: time.Now()}
					a.Finalizers = []string{"keep/me"}
					return a
				}(),
			},
			wantDeleted: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := testClients{
				coordination: testFakeClient(t, tc.assignments...),
				staging:      testFakeClient(t),
			}
			opts := testOptions(t, clients, nil)
			opts.RequeueInterval = DefaultRequeueInterval
			s := &assignmentSubroutine{opts: opts}

			ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)
			result, err := s.Finalize(ctx, testConsumerObject())
			require.NoError(t, err)

			if tc.wantOK {
				assert.True(t, result.IsContinue())
				assert.Zero(t, result.Requeue())
			} else {
				assert.Positive(t, result.Requeue())
			}

			assignment := &pmcoordbrokerv1alpha1.Assignment{}
			err = clients.coordination.Get(ctx, ctrlruntimeclient.ObjectKey{Name: testAssignmentName()}, assignment)
			if tc.wantDeleted {
				assert.True(t, ctrlruntimeclient.IgnoreNotFound(err) == nil && err != nil)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAssignmentName(t *testing.T) {
	t.Parallel()

	name := testAssignmentName()
	assert.Regexp(t, "^assignment-[0-9a-f]{16}$", name)
	assert.Equal(t, name, assignmentName(testConsumerCluster, testGVR, testNamespace, testResourceName))
	assert.NotEqual(t, name, assignmentName("other", testGVR, testNamespace, testResourceName))
}
