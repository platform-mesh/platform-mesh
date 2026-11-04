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

package subroutine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func TestWorkspaceInitializer_TerminateDeletesStoreAndWaitsForFinalizers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sub, cl := newWorkspaceInitializerTerminateSubroutine(t,
		orgStore(orgTerminateTestOrg, "core.platform-mesh.io/fga-store"),
	)

	result, err := sub.Terminate(ctx, logicalClusterForOrg(orgTerminateTestOrg))

	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
	assert.Equal(t, orgResourceDeleteRequeue, result.Requeue())

	var store pmcorev1alpha1.Store
	requireDeleting(t, cl, &store, orgTerminateTestOrg)
}

func TestWorkspaceInitializer_TerminateReturnsOKWhenStoreIsGone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sub, _ := newWorkspaceInitializerTerminateSubroutine(t)

	result, err := sub.Terminate(ctx, logicalClusterForOrg(orgTerminateTestOrg))

	require.NoError(t, err)
	assert.Equal(t, subroutines.OK(), result)
}

func TestWorkspaceInitializer_TerminateRequiresWorkspaceName(t *testing.T) {
	t.Parallel()

	sub := &workspaceInitializer{kcpClientGetter: mocks.NewMockKCPClientGetter(t)}

	result, err := sub.Terminate(context.Background(), &kcpcorev1alpha1.LogicalCluster{})

	require.Error(t, err)
	assert.Equal(t, subroutines.OK(), result)
}

func TestWorkspaceInitializer_InitializeWaitsForAccountInfo(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		getErr  error
		wantErr bool
	}{
		"missing AccountInfo requeues": {
			getErr: apierrors.NewNotFound(
				schema.GroupResource{Group: "core.platform-mesh.io", Resource: "accountinfos"},
				"account",
			),
		},
		// A no-match error is what an unbound workspace answers; it is not a reconcile failure.
		"unbound core.platform-mesh.io APIs requeue": {
			getErr: &meta.NoKindMatchError{
				GroupKind:        schema.GroupKind{Group: "core.platform-mesh.io", Kind: "AccountInfo"},
				SearchedVersions: []string{"v1alpha1"},
			},
		},
		"other errors are reported": {
			getErr:  assert.AnError,
			wantErr: true,
		},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			mgr := mocks.NewMockManager(t)
			cluster := mocks.NewMockCluster(t)
			clusterClient := mocks.NewMockClient(t)
			mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
			cluster.EXPECT().GetClient().Return(clusterClient)
			clusterClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(cas.getErr)

			sub := &workspaceInitializer{
				mgr:             mgr,
				kcpClientGetter: mocks.NewMockKCPClientGetter(t),
			}

			result, err := sub.Initialize(context.Background(), logicalClusterForOrg(orgTerminateTestOrg))

			if cas.wantErr {
				require.Error(t, err)
				assert.Equal(t, subroutines.OK(), result)
				return
			}

			require.NoError(t, err)
			assert.True(t, result.IsStopWithRequeue(), "the chain must wait for AccountInfo instead of failing")
			assert.Equal(t, 5*time.Second, result.Requeue())
		})
	}
}

func newWorkspaceInitializerTerminateSubroutine(t *testing.T, objs ...ctrlruntimeclient.Object) (*workspaceInitializer, ctrlruntimeclient.Client) {
	t.Helper()

	cl := newOrgResourceFakeClient(t, objs...)
	clientGetter := mocks.NewMockKCPClientGetter(t)
	clientGetter.EXPECT().NewClientForLogicalCluster(context.Background(), config.OrgsClusterPath).Return(cl, nil).Once()

	return &workspaceInitializer{kcpClientGetter: clientGetter}, cl
}
