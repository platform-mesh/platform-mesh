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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

func testExport(claims ...kcpapisv1alpha2.PermissionClaim) *kcpapisv1alpha2.APIExport {
	return &kcpapisv1alpha2.APIExport{
		ObjectMeta: metav1.ObjectMeta{Name: testExportName},
		Spec: kcpapisv1alpha2.APIExportSpec{
			PermissionClaims: claims,
		},
	}
}

func testClaim(resource string) kcpapisv1alpha2.PermissionClaim {
	return kcpapisv1alpha2.PermissionClaim{
		GroupResource: kcpapisv1alpha2.GroupResource{Resource: resource},
		Verbs:         []string{"*"},
	}
}

func boundBinding(claims ...kcpapisv1alpha2.AcceptablePermissionClaim) *kcpapisv1alpha2.APIBinding {
	return &kcpapisv1alpha2.APIBinding{
		ObjectMeta: metav1.ObjectMeta{Name: testExportName},
		Spec: kcpapisv1alpha2.APIBindingSpec{
			Reference: kcpapisv1alpha2.BindingReference{
				Export: &kcpapisv1alpha2.ExportBindingReference{
					Path: testProviderCluster,
					Name: testExportName,
				},
			},
			PermissionClaims: claims,
		},
		Status: kcpapisv1alpha2.APIBindingStatus{
			Phase: kcpapisv1alpha2.APIBindingPhaseBound,
		},
	}
}

func acceptedClaim(claim kcpapisv1alpha2.PermissionClaim) kcpapisv1alpha2.AcceptablePermissionClaim {
	return kcpapisv1alpha2.AcceptablePermissionClaim{
		ScopedPermissionClaim: kcpapisv1alpha2.ScopedPermissionClaim{
			PermissionClaim: claim,
			Selector: kcpapisv1alpha2.PermissionClaimSelector{
				MatchAll: true,
			},
		},
		State: kcpapisv1alpha2.ClaimAccepted,
	}
}

func TestBindingReadyGetName(t *testing.T) {
	s := &bindingReadySubroutine{}
	assert.Equal(t, pmcoordbrokerv1alpha1.StagingWorkspaceConditionBindingReady, s.GetName())
}

func TestBindingReadyProcess(t *testing.T) {
	tests := []struct {
		name         string
		stagingObjs  []ctrlruntimeclient.Object
		providerObjs []ctrlruntimeclient.Object
		wantPending  bool
		wantOK       bool
		wantErr      bool
		wantPhase    pmcoordbrokerv1alpha1.StagingWorkspacePhase
		verify       func(t *testing.T, stagingClient ctrlruntimeclient.Client)
	}{
		{
			name:    "missing provider export errors",
			wantErr: true,
		},
		{
			name:         "creates binding",
			providerObjs: []ctrlruntimeclient.Object{testExport()},
			wantPending:  true,
			wantPhase:    pmcoordbrokerv1alpha1.StagingWorkspacePhasePending,
			verify: func(t *testing.T, stagingClient ctrlruntimeclient.Client) {
				binding := &kcpapisv1alpha2.APIBinding{}
				require.NoError(t, stagingClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: testExportName}, binding))
				require.NotNil(t, binding.Spec.Reference.Export)
				assert.Equal(t, testProviderCluster, binding.Spec.Reference.Export.Path)
				assert.Equal(t, testExportName, binding.Spec.Reference.Export.Name)
				assert.Empty(t, binding.Spec.PermissionClaims)
			},
		},
		{
			name:         "creates binding with mirrored claims",
			providerObjs: []ctrlruntimeclient.Object{testExport(testClaim("configmaps"))},
			wantPending:  true,
			wantPhase:    pmcoordbrokerv1alpha1.StagingWorkspacePhasePending,
			verify: func(t *testing.T, stagingClient ctrlruntimeclient.Client) {
				binding := &kcpapisv1alpha2.APIBinding{}
				require.NoError(t, stagingClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: testExportName}, binding))
				require.Len(t, binding.Spec.PermissionClaims, 1)
				assert.Equal(t, acceptedClaim(testClaim("configmaps")), binding.Spec.PermissionClaims[0])
			},
		},
		{
			name:         "updates binding on claim drift",
			providerObjs: []ctrlruntimeclient.Object{testExport(testClaim("secrets"))},
			stagingObjs:  []ctrlruntimeclient.Object{boundBinding(acceptedClaim(testClaim("configmaps")))},
			wantPending:  true,
			wantPhase:    pmcoordbrokerv1alpha1.StagingWorkspacePhasePending,
			verify: func(t *testing.T, stagingClient ctrlruntimeclient.Client) {
				binding := &kcpapisv1alpha2.APIBinding{}
				require.NoError(t, stagingClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: testExportName}, binding))
				require.Len(t, binding.Spec.PermissionClaims, 1)
				assert.Equal(t, acceptedClaim(testClaim("secrets")), binding.Spec.PermissionClaims[0])
			},
		},
		{
			name:         "waits for binding to become bound",
			providerObjs: []ctrlruntimeclient.Object{testExport()},
			stagingObjs: []ctrlruntimeclient.Object{&kcpapisv1alpha2.APIBinding{
				ObjectMeta: metav1.ObjectMeta{Name: testExportName},
				Spec: kcpapisv1alpha2.APIBindingSpec{
					Reference: kcpapisv1alpha2.BindingReference{
						Export: &kcpapisv1alpha2.ExportBindingReference{
							Path: testProviderCluster,
							Name: testExportName,
						},
					},
				},
			}},
			wantPending: true,
			wantPhase:   pmcoordbrokerv1alpha1.StagingWorkspacePhasePending,
		},
		{
			name:         "bound binding sets phase ready",
			providerObjs: []ctrlruntimeclient.Object{testExport(testClaim("configmaps"))},
			stagingObjs:  []ctrlruntimeclient.Object{boundBinding(acceptedClaim(testClaim("configmaps")))},
			wantOK:       true,
			wantPhase:    pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, clients := testOptions(t, nil, tt.stagingObjs, tt.providerObjs)
			s := &bindingReadySubroutine{opts: opts}
			sw := testStagingWorkspace()

			result, err := s.Process(t.Context(), sw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPending, result.IsPending())
			if tt.wantOK {
				assert.True(t, result.IsContinue())
				assert.Zero(t, result.Requeue())
			}
			assert.Equal(t, tt.wantPhase, sw.Status.Phase)
			if tt.verify != nil {
				tt.verify(t, clients.staging)
			}
		})
	}
}
