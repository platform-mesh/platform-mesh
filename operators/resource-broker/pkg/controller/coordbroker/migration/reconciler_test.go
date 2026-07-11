/*
Copyright 2025.

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

package migration

import (
	"testing"
	"time"

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
	testConsumerCluster     = "consumer-cluster"
	testFromProviderCluster = "from-provider"
	testToProviderCluster   = "to-provider"
	testExportName          = "example-export"
	testAcceptAPIName       = "accept-widgets"
	testAssignmentName      = "assignment-abc123"
	testMigrationName       = "migration-abc123"
	testTreeRoot            = "root:staging"
	testFromStagingName     = "staging-from"
	testToStagingName       = "staging-to"
	testNamespace           = "default"
	testResourceName        = "my-widget"
)

var (
	testGVK = metav1.GroupVersionKind{Group: "example.io", Version: "v1", Kind: "Widget"}

	testSchemaGVK = schema.GroupVersionKind{Group: "example.io", Version: "v1", Kind: "Widget"}
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	s := runtime.NewScheme()
	require.NoError(t, pmbrokerv1alpha1.AddToScheme(s))
	require.NoError(t, pmcoordbrokerv1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))
	s.AddKnownTypeWithName(testSchemaGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(testSchemaGVK.GroupVersion().WithKind(testSchemaGVK.Kind+"List"), &unstructured.UnstructuredList{})
	return s
}

func testFakeClient(t *testing.T, objs ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	t.Helper()

	widget := &unstructured.Unstructured{}
	widget.SetGroupVersionKind(testSchemaGVK)
	return fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(objs...).
		WithStatusSubresource(widget, &pmcoordbrokerv1alpha1.Assignment{}, &pmcoordbrokerv1alpha1.StagingWorkspace{}).
		Build()
}

type testClients struct {
	coordination ctrlruntimeclient.Client
	provider     ctrlruntimeclient.Client
	fromStaging  ctrlruntimeclient.Client
	toStaging    ctrlruntimeclient.Client
	compute      ctrlruntimeclient.Client
}

func testOptions(t *testing.T, clients *testClients) Options {
	t.Helper()

	return Options{
		ComputeClient:   clients.compute,
		StagingTreeRoot: testTreeRoot,
		StageNamespace:  DefaultStageNamespace,
		RequeueInterval: time.Second,
		WorkspaceClientFunc: func(path string) (ctrlruntimeclient.Client, error) {
			switch path {
			case testTreeRoot + ":" + testFromStagingName:
				return clients.fromStaging, nil
			case testTreeRoot + ":" + testToStagingName:
				return clients.toStaging, nil
			default:
				t.Fatalf("unexpected workspace client path %q", path)
				return nil, nil
			}
		},
	}
}

func testMigration(state pmcoordbrokerv1alpha1.MigrationState) *pmcoordbrokerv1alpha1.Migration {
	return &pmcoordbrokerv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name: testMigrationName,
		},
		Spec: pmcoordbrokerv1alpha1.MigrationSpec{
			Assignment:           testAssignmentName,
			Namespace:            testNamespace,
			Name:                 testResourceName,
			FromStagingWorkspace: testFromStagingName,
			StagingWorkspace:     testToStagingName,
			From: pmcoordbrokerv1alpha1.MigrationTarget{
				GVK:             testGVK,
				ProviderCluster: testFromProviderCluster,
				AcceptAPIName:   "accept-widgets-from",
			},
			To: pmcoordbrokerv1alpha1.MigrationTarget{
				GVK:             testGVK,
				ProviderCluster: testToProviderCluster,
				AcceptAPIName:   testAcceptAPIName,
			},
		},
		Status: pmcoordbrokerv1alpha1.MigrationStatus{
			State: state,
		},
	}
}

// testStagingWidget returns the staging copy of the consumer object with the
// given status.status value. Empty status omits the field.
func testStagingWidget(status string) *unstructured.Unstructured {
	widget := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "example.io/v1",
			"kind":       "Widget",
			"metadata": map[string]any{
				"name":      testResourceName,
				"namespace": testNamespace,
			},
			"spec": map[string]any{
				"size": int64(3),
			},
		},
	}
	if status != "" {
		_ = unstructured.SetNestedField(widget.Object, status, "status", "status")
	}
	return widget
}

func TestNewReconcilerValidation(t *testing.T) {
	t.Parallel()

	valid := func(t *testing.T) Options {
		t.Helper()
		clients := &testClients{
			coordination: testFakeClient(t),
			provider:     testFakeClient(t),
			fromStaging:  testFakeClient(t),
			toStaging:    testFakeClient(t),
			compute:      testFakeClient(t),
		}
		return testOptions(t, clients)
	}

	tests := []struct {
		name    string
		mutate  func(*Options)
		wantErr string
	}{
		{
			name:    "missing compute client",
			mutate:  func(opts *Options) { opts.ComputeClient = nil },
			wantErr: "options: ComputeClient is required",
		},
		{
			name:    "missing workspace client func",
			mutate:  func(opts *Options) { opts.WorkspaceClientFunc = nil },
			wantErr: "options: WorkspaceClientFunc is required",
		},
		{
			name:    "missing staging tree root",
			mutate:  func(opts *Options) { opts.StagingTreeRoot = "" },
			wantErr: "options: StagingTreeRoot is required",
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
			require.NotNil(t, r)
		})
	}
}
