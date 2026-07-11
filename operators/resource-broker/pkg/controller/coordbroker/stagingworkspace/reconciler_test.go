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

package stagingworkspace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	testConsumerCluster = "consumer-cluster"
	testProviderCluster = "provider-cluster"
	testExportName      = "example-export"
	testTreeRoot        = "root:staging"
	testName            = "staging-abc123"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, pmcoordbrokerv1alpha1.AddToScheme(scheme))
	require.NoError(t, kcptenancyv1alpha1.AddToScheme(scheme))
	require.NoError(t, kcpapisv1alpha2.AddToScheme(scheme))
	return scheme
}

func testStagingWorkspace() *pmcoordbrokerv1alpha1.StagingWorkspace {
	return &pmcoordbrokerv1alpha1.StagingWorkspace{
		ObjectMeta: metav1.ObjectMeta{Name: testName},
		Spec: pmcoordbrokerv1alpha1.StagingWorkspaceSpec{
			ConsumerCluster: testConsumerCluster,
			ProviderCluster: testProviderCluster,
			APIExportName:   testExportName,
		},
	}
}

type testClients struct {
	tree     ctrlruntimeclient.Client
	staging  ctrlruntimeclient.Client
	provider ctrlruntimeclient.Client
}

func testOptions(t *testing.T, treeObjs, stagingObjs, providerObjs []ctrlruntimeclient.Object) (Options, *testClients) {
	t.Helper()
	scheme := testScheme(t)
	clients := &testClients{
		tree:     fake.NewClientBuilder().WithScheme(scheme).WithObjects(treeObjs...).Build(),
		staging:  fake.NewClientBuilder().WithScheme(scheme).WithObjects(stagingObjs...).Build(),
		provider: fake.NewClientBuilder().WithScheme(scheme).WithObjects(providerObjs...).Build(),
	}
	opts := Options{
		StagingTreeRoot: testTreeRoot,
		RequeueInterval: time.Second,
		WorkspaceClientFunc: func(path string) (ctrlruntimeclient.Client, error) {
			switch path {
			case testTreeRoot:
				return clients.tree, nil
			case testProviderCluster:
				return clients.provider, nil
			default:
				return clients.staging, nil
			}
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
			name:    "missing staging tree root",
			opts:    Options{WorkspaceClientFunc: clientFunc},
			wantErr: true,
		},
		{
			name:    "missing workspace client func",
			opts:    Options{StagingTreeRoot: testTreeRoot},
			wantErr: true,
		},
		{
			name: "valid options",
			opts: Options{StagingTreeRoot: testTreeRoot, WorkspaceClientFunc: clientFunc},
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
