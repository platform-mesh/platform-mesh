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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testConsumerCluster = "consumer-cluster"
	testProviderCluster = "provider-cluster"
	testExportName      = "example-export"
	testAcceptAPIName   = "accept-widgets"
	testStagingName     = "staging-abc123"
	testTreeRoot        = "root:staging"
	testNamespace       = "default"
	testResourceName    = "my-widget"
)

var (
	testGVK = schema.GroupVersionKind{Group: "example.io", Version: "v1", Kind: "Widget"}
	testGVR = metav1.GroupVersionResource{Group: "example.io", Version: "v1", Resource: "widgets"}
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, pmbrokerv1alpha1.AddToScheme(s))
	require.NoError(t, pmcoordbrokerv1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))
	s.AddKnownTypeWithName(testGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(testGVK.GroupVersion().WithKind(testGVK.Kind+"List"), &unstructured.UnstructuredList{})
	return s
}

func testFakeClient(t *testing.T, objs ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	t.Helper()
	statusObj := &unstructured.Unstructured{}
	statusObj.SetGroupVersionKind(testGVK)
	return fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(objs...).
		WithStatusSubresource(statusObj, &pmcoordbrokerv1alpha1.Assignment{}).
		Build()
}

// testClients bundles the fake clients backing testOptions.
type testClients struct {
	coordination ctrlruntimeclient.Client
	staging      ctrlruntimeclient.Client
}

func testOptions(t *testing.T, clients testClients, refs []AcceptAPIRef) Options {
	t.Helper()
	return Options{
		GVK:                testGVK,
		GVR:                testGVR,
		StagingTreeRoot:    testTreeRoot,
		CoordinationClient: clients.coordination,
		WorkspaceClientFunc: func(path string) (ctrlruntimeclient.Client, error) {
			assert.Equal(t, testTreeRoot+":"+testStagingName, path)
			return clients.staging, nil
		},
		ListAcceptAPIs: func(_ context.Context) ([]AcceptAPIRef, error) {
			return refs, nil
		},
	}
}

func testConsumerObject() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.io/v1",
		"kind":       "Widget",
		"metadata": map[string]any{
			"name":      testResourceName,
			"namespace": testNamespace,
		},
		"spec": map[string]any{"size": int64(3)},
	}}
	return obj
}

func testAcceptAPI() *pmbrokerv1alpha1.AcceptAPI {
	return &pmbrokerv1alpha1.AcceptAPI{
		ObjectMeta: metav1.ObjectMeta{Name: testAcceptAPIName},
		Spec: pmbrokerv1alpha1.AcceptAPISpec{
			GVR:           testGVR,
			APIExportName: testExportName,
		},
	}
}

func testAssignmentName() string {
	return assignmentName(testConsumerCluster, testGVR, testNamespace, testResourceName)
}

func testAssignment(phase pmcoordbrokerv1alpha1.AssignmentPhase) *pmcoordbrokerv1alpha1.Assignment {
	return &pmcoordbrokerv1alpha1.Assignment{
		ObjectMeta: metav1.ObjectMeta{Name: testAssignmentName()},
		Spec: pmcoordbrokerv1alpha1.AssignmentSpec{
			ConsumerCluster: testConsumerCluster,
			GVR:             testGVR,
			Namespace:       testNamespace,
			Name:            testResourceName,
			ProviderCluster: testProviderCluster,
			AcceptAPIName:   testAcceptAPIName,
		},
		Status: pmcoordbrokerv1alpha1.AssignmentStatus{
			APIExportName:    testExportName,
			StagingWorkspace: testStagingName,
			Phase:            phase,
		},
	}
}

func TestNewReconcilerValidation(t *testing.T) {
	t.Parallel()

	valid := func(t *testing.T) Options {
		return testOptions(t, testClients{
			coordination: testFakeClient(t),
			staging:      testFakeClient(t),
		}, nil)
	}

	tests := []struct {
		name    string
		mutate  func(*Options)
		wantErr string
	}{
		{
			name:    "missing GVK",
			mutate:  func(o *Options) { o.GVK = schema.GroupVersionKind{} },
			wantErr: "GVK is required",
		},
		{
			name:    "missing GVR",
			mutate:  func(o *Options) { o.GVR = metav1.GroupVersionResource{} },
			wantErr: "GVR is required",
		},
		{
			name:    "missing staging tree root",
			mutate:  func(o *Options) { o.StagingTreeRoot = "" },
			wantErr: "StagingTreeRoot is required",
		},
		{
			name:    "missing workspace client func",
			mutate:  func(o *Options) { o.WorkspaceClientFunc = nil },
			wantErr: "WorkspaceClientFunc is required",
		},
		{
			name:    "missing coordination client",
			mutate:  func(o *Options) { o.CoordinationClient = nil },
			wantErr: "CoordinationClient is required",
		},
		{
			name:    "missing list accept apis",
			mutate:  func(o *Options) { o.ListAcceptAPIs = nil },
			wantErr: "ListAcceptAPIs is required",
		},
		{
			name:   "valid",
			mutate: func(*Options) {},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := valid(t)
			tc.mutate(&opts)

			r, err := NewReconciler(nil, opts)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, r)
		})
	}
}

func TestControllerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		gvr  metav1.GroupVersionResource
		want string
	}{
		{
			name: "with group",
			gvr:  testGVR,
			want: "brokeredresource-widgets.example.io",
		},
		{
			name: "core group",
			gvr:  metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			want: "brokeredresource-configmaps",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, controllerName(tc.gvr))
		})
	}
}

func TestControllerNameOverride(t *testing.T) {
	t.Parallel()

	clients := testClients{
		coordination: testFakeClient(t),
		staging:      testFakeClient(t),
	}

	opts := testOptions(t, clients, nil)
	opts.ControllerName = "brokeredresource-my-slice-widgets.example.io"

	r, err := NewReconciler(nil, opts)
	require.NoError(t, err)
	assert.Equal(t, "brokeredresource-my-slice-widgets.example.io", r.name)
}
