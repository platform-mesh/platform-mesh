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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	"go.platform-mesh.io/pm-keycloak-operator/internal/subroutine/mocks"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/config"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const orgTerminateTestOrg = "acme"

func TestWorkspaceAuthSubroutine_TerminateDeletesWAC(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sub, cl := newWorkspaceAuthTerminateSubroutine(t,
		orgWorkspaceAuthConfiguration(orgTerminateTestOrg),
	)

	result, err := sub.Terminate(ctx, logicalClusterForOrg(orgTerminateTestOrg))

	require.NoError(t, err)
	assert.Equal(t, subroutines.OK(), result)

	var authConfig kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration
	requireNotFound(t, cl, &authConfig, orgTerminateTestOrg)
}

func TestWorkspaceAuthSubroutine_TerminateReturnsOKWhenWACIsGone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sub, _ := newWorkspaceAuthTerminateSubroutine(t)

	result, err := sub.Terminate(ctx, logicalClusterForOrg(orgTerminateTestOrg))

	require.NoError(t, err)
	assert.Equal(t, subroutines.OK(), result)
}

func TestWorkspaceAuthSubroutine_TerminateRequiresWorkspaceName(t *testing.T) {
	t.Parallel()

	sub := &workspaceAuthSubroutine{kcpClientGetter: mocks.NewMockKCPClientGetter(t)}

	result, err := sub.Terminate(context.Background(), &kcpcorev1alpha1.LogicalCluster{})

	require.Error(t, err)
	assert.Equal(t, subroutines.OK(), result)
}

func newWorkspaceAuthTerminateSubroutine(t *testing.T, objs ...ctrlruntimeclient.Object) (*workspaceAuthSubroutine, ctrlruntimeclient.Client) {
	t.Helper()

	cl := newOrgResourceFakeClient(t, objs...)
	clientGetter := mocks.NewMockKCPClientGetter(t)
	clientGetter.EXPECT().NewClientForLogicalCluster(context.Background(), config.OrgsClusterPath).Return(cl, nil).Once()

	return &workspaceAuthSubroutine{kcpClientGetter: clientGetter}, cl
}

func newOrgResourceFakeClient(t *testing.T, objs ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(pmkeycloakv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

func orgWorkspaceAuthConfiguration(name string) *kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration {
	return &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func orgIdentityProvider(name string, finalizers ...string) *pmkeycloakv1alpha1.IdentityProviderConfiguration {
	return &pmkeycloakv1alpha1.IdentityProviderConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name, Finalizers: finalizers}}
}

func logicalClusterForOrg(name string) *kcpcorev1alpha1.LogicalCluster {
	return &kcpcorev1alpha1.LogicalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"kcp.io/path": "root:orgs:" + name,
			},
		},
	}
}

func requireNotFound(t *testing.T, cl ctrlruntimeclient.Client, obj ctrlruntimeclient.Object, name string) {
	t.Helper()

	err := cl.Get(context.Background(), ctrlruntimeclient.ObjectKey{Name: name}, obj)
	assert.True(t, apierrors.IsNotFound(err))
}

func requireDeleting(t *testing.T, cl ctrlruntimeclient.Client, obj ctrlruntimeclient.Object, name string) {
	t.Helper()

	require.NoError(t, cl.Get(context.Background(), ctrlruntimeclient.ObjectKey{Name: name}, obj))
	assert.NotNil(t, obj.GetDeletionTimestamp())
}
