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

package acceptapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/names"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	testCluster    = "test-cluster"
	testExportName = "example-export"
	testTreeRoot   = "root:platform"
)

func testAcceptAPI() *pmbrokerv1alpha1.AcceptAPI {
	return &pmbrokerv1alpha1.AcceptAPI{
		ObjectMeta: metav1.ObjectMeta{Name: "my-accept"},
		Spec: pmbrokerv1alpha1.AcceptAPISpec{
			APIExportName: testExportName,
		},
	}
}

func testRefFinalizer() string {
	return refFinalizerPrefix + names.Hash(testCluster, "my-accept")
}

func testSubroutine(t *testing.T, treeObjs, wsObjs []ctrlruntimeclient.Object) (*bindingVerifiedSubroutine, ctrlruntimeclient.Client, ctrlruntimeclient.Client) {
	t.Helper()
	scheme := testScheme(t)
	treeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(treeObjs...).Build()
	wsClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wsObjs...).Build()
	s := &bindingVerifiedSubroutine{opts: Options{
		VerificationTreeRoot: testTreeRoot,
		RequeueInterval:      time.Second,
		WorkspaceClientFunc: func(path string) (ctrlruntimeclient.Client, error) {
			if path == testTreeRoot {
				return treeClient, nil
			}
			return wsClient, nil
		},
	}}
	return s, treeClient, wsClient
}

func readyWorkspace(name string, finalizers ...string) *kcptenancyv1alpha1.Workspace {
	return &kcptenancyv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Finalizers: finalizers,
		},
		Status: kcptenancyv1alpha1.WorkspaceStatus{
			Phase: kcpcorev1alpha1.LogicalClusterPhaseReady,
		},
	}
}

func TestBindingVerifiedGetName(t *testing.T) {
	s := &bindingVerifiedSubroutine{}
	assert.Equal(t, pmbrokerv1alpha1.AcceptAPIConditionBindingVerified, s.GetName())
}

func TestBindingVerifiedFinalizers(t *testing.T) {
	s := &bindingVerifiedSubroutine{}
	assert.Equal(t, []string{VerificationFinalizer}, s.Finalizers(nil))
}

func TestBindingVerifiedProcess(t *testing.T) {
	wsName := workspaceName(testCluster, testExportName)

	tests := []struct {
		name        string
		acceptAPI   *pmbrokerv1alpha1.AcceptAPI
		treeObjs    []ctrlruntimeclient.Object
		wsObjs      []ctrlruntimeclient.Object
		wantPending bool
		wantOK      bool
		verify      func(t *testing.T, treeClient, wsClient ctrlruntimeclient.Client, acceptAPI *pmbrokerv1alpha1.AcceptAPI)
	}{
		{
			name:        "creates workspace with reference finalizer",
			acceptAPI:   testAcceptAPI(),
			wantPending: true,
			verify: func(t *testing.T, treeClient, _ ctrlruntimeclient.Client, acceptAPI *pmbrokerv1alpha1.AcceptAPI) {
				ws := &kcptenancyv1alpha1.Workspace{}
				require.NoError(t, treeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: wsName}, ws))
				assert.Equal(t, []string{testRefFinalizer()}, ws.Finalizers)
				assert.Equal(t, wsName, acceptAPI.Status.VerificationWorkspace)
			},
		},
		{
			name:      "adds reference finalizer to existing workspace",
			acceptAPI: testAcceptAPI(),
			treeObjs: []ctrlruntimeclient.Object{&kcptenancyv1alpha1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:       wsName,
					Finalizers: []string{refFinalizerPrefix + "other"},
				},
			}},
			wantPending: true,
			verify: func(t *testing.T, treeClient, _ ctrlruntimeclient.Client, acceptAPI *pmbrokerv1alpha1.AcceptAPI) {
				ws := &kcptenancyv1alpha1.Workspace{}
				require.NoError(t, treeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: wsName}, ws))
				assert.Contains(t, ws.Finalizers, testRefFinalizer())
				assert.Equal(t, wsName, acceptAPI.Status.VerificationWorkspace)
			},
		},
		{
			name:      "waits for workspace to become ready",
			acceptAPI: testAcceptAPI(),
			treeObjs: []ctrlruntimeclient.Object{&kcptenancyv1alpha1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:       wsName,
					Finalizers: []string{testRefFinalizer()},
				},
			}},
			wantPending: true,
		},
		{
			name:        "creates binding in ready workspace",
			acceptAPI:   testAcceptAPI(),
			treeObjs:    []ctrlruntimeclient.Object{readyWorkspace(wsName, testRefFinalizer())},
			wantPending: true,
			verify: func(t *testing.T, _, wsClient ctrlruntimeclient.Client, _ *pmbrokerv1alpha1.AcceptAPI) {
				binding := &kcpapisv1alpha2.APIBinding{}
				require.NoError(t, wsClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: testExportName}, binding))
				require.NotNil(t, binding.Spec.Reference.Export)
				assert.Equal(t, testCluster, binding.Spec.Reference.Export.Path)
				assert.Equal(t, testExportName, binding.Spec.Reference.Export.Name)
			},
		},
		{
			name:      "waits for binding to become bound",
			acceptAPI: testAcceptAPI(),
			treeObjs:  []ctrlruntimeclient.Object{readyWorkspace(wsName, testRefFinalizer())},
			wsObjs: []ctrlruntimeclient.Object{&kcpapisv1alpha2.APIBinding{
				ObjectMeta: metav1.ObjectMeta{Name: testExportName},
			}},
			wantPending: true,
		},
		{
			name:      "bound binding completes",
			acceptAPI: testAcceptAPI(),
			treeObjs:  []ctrlruntimeclient.Object{readyWorkspace(wsName, testRefFinalizer())},
			wsObjs: []ctrlruntimeclient.Object{&kcpapisv1alpha2.APIBinding{
				ObjectMeta: metav1.ObjectMeta{Name: testExportName},
				Status: kcpapisv1alpha2.APIBindingStatus{
					Phase: kcpapisv1alpha2.APIBindingPhaseBound,
				},
			}},
			wantOK: true,
		},
		{
			name: "spec change releases previous workspace",
			acceptAPI: func() *pmbrokerv1alpha1.AcceptAPI {
				acceptAPI := testAcceptAPI()
				acceptAPI.Status.VerificationWorkspace = "verify-old"
				return acceptAPI
			}(),
			treeObjs: []ctrlruntimeclient.Object{
				&kcptenancyv1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "verify-old",
						Finalizers: []string{testRefFinalizer()},
					},
				},
				readyWorkspace(wsName, testRefFinalizer()),
			},
			wsObjs: []ctrlruntimeclient.Object{&kcpapisv1alpha2.APIBinding{
				ObjectMeta: metav1.ObjectMeta{Name: testExportName},
				Status: kcpapisv1alpha2.APIBindingStatus{
					Phase: kcpapisv1alpha2.APIBindingPhaseBound,
				},
			}},
			wantOK: true,
			verify: func(t *testing.T, treeClient, _ ctrlruntimeclient.Client, acceptAPI *pmbrokerv1alpha1.AcceptAPI) {
				ws := &kcptenancyv1alpha1.Workspace{}
				err := treeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "verify-old"}, ws)
				assert.True(t, ctrlruntimeclient.IgnoreNotFound(err) == nil && err != nil, "old workspace should be deleted, got: %v", err)
				assert.Equal(t, wsName, acceptAPI.Status.VerificationWorkspace)
			},
		},
		{
			name: "spec change keeps previous workspace with other references",
			acceptAPI: func() *pmbrokerv1alpha1.AcceptAPI {
				acceptAPI := testAcceptAPI()
				acceptAPI.Status.VerificationWorkspace = "verify-old"
				return acceptAPI
			}(),
			treeObjs: []ctrlruntimeclient.Object{
				&kcptenancyv1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "verify-old",
						Finalizers: []string{testRefFinalizer(), refFinalizerPrefix + "other"},
					},
				},
			},
			wantPending: true,
			verify: func(t *testing.T, treeClient, _ ctrlruntimeclient.Client, _ *pmbrokerv1alpha1.AcceptAPI) {
				ws := &kcptenancyv1alpha1.Workspace{}
				require.NoError(t, treeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "verify-old"}, ws))
				assert.Equal(t, []string{refFinalizerPrefix + "other"}, ws.Finalizers)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, treeClient, wsClient := testSubroutine(t, tt.treeObjs, tt.wsObjs)
			ctx := mccontext.WithCluster(t.Context(), testCluster)

			result, err := s.Process(ctx, tt.acceptAPI)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPending, result.IsPending())
			if tt.wantOK {
				assert.True(t, result.IsContinue())
				assert.Zero(t, result.Requeue())
			}
			if tt.verify != nil {
				tt.verify(t, treeClient, wsClient, tt.acceptAPI)
			}
		})
	}
}

func TestBindingVerifiedProcessTerminatingWorkspace(t *testing.T) {
	wsName := workspaceName(testCluster, testExportName)
	s, treeClient, _ := testSubroutine(t, []ctrlruntimeclient.Object{
		readyWorkspace(wsName, refFinalizerPrefix+"other"),
	}, nil)

	ws := &kcptenancyv1alpha1.Workspace{}
	require.NoError(t, treeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: wsName}, ws))
	require.NoError(t, treeClient.Delete(t.Context(), ws))

	ctx := mccontext.WithCluster(t.Context(), testCluster)
	result, err := s.Process(ctx, testAcceptAPI())
	require.NoError(t, err)
	assert.True(t, result.IsPending())
}

func TestBindingVerifiedProcessNoClusterInContext(t *testing.T) {
	s, _, _ := testSubroutine(t, nil, nil)
	_, err := s.Process(t.Context(), testAcceptAPI())
	assert.Error(t, err)
}

func TestBindingVerifiedFinalize(t *testing.T) {
	tests := []struct {
		name      string
		acceptAPI *pmbrokerv1alpha1.AcceptAPI
		treeObjs  []ctrlruntimeclient.Object
		verify    func(t *testing.T, treeClient ctrlruntimeclient.Client)
	}{
		{
			name: "no verification workspace recorded",
			acceptAPI: func() *pmbrokerv1alpha1.AcceptAPI {
				acceptAPI := testAcceptAPI()
				acceptAPI.Status.VerificationWorkspace = ""
				return acceptAPI
			}(),
		},
		{
			name: "workspace already gone",
			acceptAPI: func() *pmbrokerv1alpha1.AcceptAPI {
				acceptAPI := testAcceptAPI()
				acceptAPI.Status.VerificationWorkspace = "verify-gone"
				return acceptAPI
			}(),
		},
		{
			name: "last reference deletes workspace",
			acceptAPI: func() *pmbrokerv1alpha1.AcceptAPI {
				acceptAPI := testAcceptAPI()
				acceptAPI.Status.VerificationWorkspace = "verify-ws"
				return acceptAPI
			}(),
			treeObjs: []ctrlruntimeclient.Object{&kcptenancyv1alpha1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "verify-ws",
					Finalizers: []string{testRefFinalizer()},
				},
			}},
			verify: func(t *testing.T, treeClient ctrlruntimeclient.Client) {
				ws := &kcptenancyv1alpha1.Workspace{}
				err := treeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "verify-ws"}, ws)
				assert.True(t, ctrlruntimeclient.IgnoreNotFound(err) == nil && err != nil, "workspace should be deleted, got: %v", err)
			},
		},
		{
			name: "remaining references keep workspace",
			acceptAPI: func() *pmbrokerv1alpha1.AcceptAPI {
				acceptAPI := testAcceptAPI()
				acceptAPI.Status.VerificationWorkspace = "verify-ws"
				return acceptAPI
			}(),
			treeObjs: []ctrlruntimeclient.Object{&kcptenancyv1alpha1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "verify-ws",
					Finalizers: []string{testRefFinalizer(), refFinalizerPrefix + "other"},
				},
			}},
			verify: func(t *testing.T, treeClient ctrlruntimeclient.Client) {
				ws := &kcptenancyv1alpha1.Workspace{}
				require.NoError(t, treeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "verify-ws"}, ws))
				assert.Equal(t, []string{refFinalizerPrefix + "other"}, ws.Finalizers)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, treeClient, _ := testSubroutine(t, tt.treeObjs, nil)
			ctx := mccontext.WithCluster(t.Context(), testCluster)

			result, err := s.Finalize(ctx, tt.acceptAPI)
			require.NoError(t, err)
			assert.True(t, result.IsContinue())
			assert.Empty(t, tt.acceptAPI.Status.VerificationWorkspace)

			if tt.verify != nil {
				tt.verify(t, treeClient)
			}
		})
	}
}

func TestWorkspaceName(t *testing.T) {
	name := workspaceName(testCluster, testExportName)
	assert.Equal(t, workspaceNamePrefix+names.Hash(testCluster, testExportName), name)
	assert.Equal(t, name, workspaceName(testCluster, testExportName))
	assert.NotEqual(t, name, workspaceName(testCluster, "other-export"))
}
