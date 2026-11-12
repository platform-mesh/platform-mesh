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

// Package tenant holds black-box e2e scenarios of what a tenant does on a
// deployed Platform Mesh (see tests/framework/pm for how it is found):
// creating an organization, accounts in it and users for it.
package tenant

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/keycloak"
	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// TestOrganizationAccountAndUsers: an organization created for a user gets
// a workspace, a Keycloak realm and that user in it; an account in it gets a
// workspace; an invite puts a second user into the realm; and the users can
// be given a password. Account and organization can be deleted again.
func TestOrganizationAccountAndUsers(t *testing.T) {
	const (
		account  = "team-a"
		password = "MyPass1234"
	)
	ctx := t.Context()
	kcp := pm.KCP(t, "")
	kc := pm.Keycloak(t)

	// Runs share the cluster: each gets an organization and users of its own.
	id := rand.String(8)
	org := "e2e-tenant-" + id
	owner := fmt.Sprintf("owner-%s@example.com", id)
	invited := fmt.Sprintf("invited-%s@example.com", id)
	t.Logf("organization %s, owner %s", org, owner)

	orgs := tenant.Client(t, kcp, tenant.OrgsPath)
	t.Cleanup(func() {
		// What is left when the test fails half-way; the context of the
		// test is done by now. The organization is only asked to go.
		ctx := context.Background()
		err := orgs.Delete(ctx, &pmcorev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: org}})
		if err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("deleting organization %s: %v", org, err)
		}
		if err := kc.DeleteUser(ctx, keycloak.WelcomeRealm, owner); err != nil {
			t.Errorf("deleting user %s: %v", owner, err)
		}
	})

	created := tenant.EnsureOrganization(t, kcp, org, owner)
	require.Equal(t, pmcorev1alpha1.AccountTypeOrg, created.Spec.Type)
	requireWorkspaceReady(t, orgs, org)

	// The creator is invited by Platform Mesh and has to set a password
	// on the first login; EnsureUser does that for them.
	user, err := kc.User(ctx, org, owner)
	require.NoError(t, err, "the creator should be a user of the realm of the organization")
	ownerID, err := kc.EnsureUser(ctx, org, keycloak.User{Email: owner, FirstName: "Owner", LastName: "User", Password: password})
	require.NoError(t, err)
	require.Equal(t, user.ID, ownerID, "EnsureUser should keep the invited user")
	user, err = kc.User(ctx, org, owner)
	require.NoError(t, err)
	require.True(t, user.Enabled)
	require.Empty(t, user.RequiredActions, "the creator should be able to log in without further steps")

	tenant.EnsureAccount(t, kcp, org, account, owner)
	requireWorkspaceReady(t, tenant.Client(t, kcp, tenant.OrganizationPath(org)), account)
	// The account workspace answers.
	require.NoError(t, tenant.Client(t, kcp, tenant.AccountPath(org, account)).List(ctx, &pmcorev1alpha1.AccountInfoList{}))

	tenant.EnsureInvite(t, kcp, org, "invited", invited)
	_, err = kc.User(ctx, org, invited)
	require.NoError(t, err, "the invited user should be a user of the realm of the organization")

	// Everything is idempotent.
	tenant.EnsureOrganization(t, kcp, org, owner)
	tenant.EnsureAccount(t, kcp, org, account, owner)
	tenant.EnsureInvite(t, kcp, org, "invited", invited)
	again, err := kc.EnsureUser(ctx, org, keycloak.User{Email: owner, Password: password})
	require.NoError(t, err)
	require.Equal(t, ownerID, again)

	// A user Platform Mesh did not create.
	_, err = kc.User(ctx, keycloak.WelcomeRealm, owner)
	require.ErrorIs(t, err, keycloak.ErrNotFound)
	_, err = kc.EnsureUser(ctx, keycloak.WelcomeRealm, keycloak.User{Email: owner, Password: password})
	require.NoError(t, err)
	_, err = kc.User(ctx, keycloak.WelcomeRealm, owner)
	require.NoError(t, err)

	// The account goes with its workspace.
	tenant.DeleteAccount(t, kcp, org, account)
	err = tenant.Client(t, kcp, tenant.OrganizationPath(org)).Get(ctx, ctrlruntimeclient.ObjectKey{Name: account}, &kcptenancyv1alpha1.Workspace{})
	require.True(t, apierrors.IsNotFound(err), "the workspace of the account should be gone, got: %v", err)

	// The organization goes with its workspace and its realm.
	tenant.DeleteOrganization(t, kcp, org)
	err = orgs.Get(ctx, ctrlruntimeclient.ObjectKey{Name: org}, &kcptenancyv1alpha1.Workspace{})
	require.True(t, apierrors.IsNotFound(err), "the workspace of the organization should be gone, got: %v", err)
	_, err = kc.User(ctx, org, owner)
	require.Error(t, err, "the realm of the organization should be gone")
	require.NotErrorIs(t, err, keycloak.ErrNotFound, "the realm of the organization should be gone")
}

func requireWorkspaceReady(t *testing.T, cl ctrlruntimeclient.Client, name string) {
	t.Helper()
	wait.Eventually(t, func() (bool, string) {
		ws := &kcptenancyv1alpha1.Workspace{}
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, ws); err != nil {
			return false, fmt.Sprintf("getting workspace: %v", err)
		}
		return ws.Status.Phase == "Ready", fmt.Sprintf("phase %q", ws.Status.Phase)
	}, time.Minute, time.Second, "workspace %s should be Ready", name)
}
