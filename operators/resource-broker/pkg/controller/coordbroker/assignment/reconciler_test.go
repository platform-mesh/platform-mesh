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

package assignment

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testConsumerCluster = "consumer-cluster"
	testProviderCluster = "provider-cluster"
	testExportName      = "example-export"
	testAcceptAPIName   = "accept-widgets"
	testAssignmentName  = "assignment-abc123"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, pmbrokerv1alpha1.AddToScheme(scheme))
	require.NoError(t, pmcoordbrokerv1alpha1.AddToScheme(scheme))
	return scheme
}

func testAssignment() *pmcoordbrokerv1alpha1.Assignment {
	return &pmcoordbrokerv1alpha1.Assignment{
		ObjectMeta: metav1.ObjectMeta{Name: testAssignmentName},
		Spec: pmcoordbrokerv1alpha1.AssignmentSpec{
			ConsumerCluster: testConsumerCluster,
			GVR: metav1.GroupVersionResource{
				Group:    "example.io",
				Version:  "v1",
				Resource: "widgets",
			},
			Namespace:       "default",
			Name:            "my-widget",
			ProviderCluster: testProviderCluster,
			AcceptAPIName:   testAcceptAPIName,
		},
	}
}

func testAcceptAPI() *pmbrokerv1alpha1.AcceptAPI {
	return &pmbrokerv1alpha1.AcceptAPI{
		ObjectMeta: metav1.ObjectMeta{Name: testAcceptAPIName},
		Spec: pmbrokerv1alpha1.AcceptAPISpec{
			GVR: metav1.GroupVersionResource{
				Group:    "example.io",
				Version:  "v1",
				Resource: "widgets",
			},
			APIExportName: testExportName,
		},
	}
}

type testClients struct {
	coordination ctrlruntimeclient.Client
	provider     ctrlruntimeclient.Client
}

func testOptions(t *testing.T, coordObjs, providerObjs []ctrlruntimeclient.Object) (Options, *testClients) {
	t.Helper()
	scheme := testScheme(t)
	clients := &testClients{
		coordination: fake.NewClientBuilder().WithScheme(scheme).WithObjects(coordObjs...).Build(),
		provider:     fake.NewClientBuilder().WithScheme(scheme).WithObjects(providerObjs...).Build(),
	}
	opts := Options{
		RequeueInterval: time.Second,
		WorkspaceClientFunc: func(path string) (ctrlruntimeclient.Client, error) {
			require.Equal(t, testProviderCluster, path)
			return clients.provider, nil
		},
	}
	return opts, clients
}

func TestNewReconcilerValidation(t *testing.T) {
	clientFunc := func(string) (ctrlruntimeclient.Client, error) { return nil, nil }

	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{
			name:    "missing workspace client func",
			opts:    Options{},
			wantErr: true,
		},
		{
			name: "valid options",
			opts: Options{WorkspaceClientFunc: clientFunc},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewReconciler(nil, tt.opts)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, r)
		})
	}
}
