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

// Package tenant creates what a tenant of a deployed Platform Mesh owns: an
// organization, the accounts in it and the users invited to it. It does what
// the portal does on behalf of a user, as the kcp admin:
//
//	kcp := pm.KCP(t, "")
//	tenant.EnsureOrganization(t, kcp, "acme", "owner@example.com")
//	tenant.EnsureAccount(t, kcp, "acme", "team-a", "owner@example.com")
//	tenant.EnsureInvite(t, kcp, "acme", "colleague", "colleague@example.com")
//
// Every function takes the kcp admin config without a workspace path, is
// idempotent, and returns once the object is Ready. The users end up in the
// Keycloak realm named like the organization; see the keycloak package for
// giving them a password.
//
// Organizations and accounts are created as their creator, whom the admin
// impersonates: Platform Mesh records whoever sent the request as the
// creator, makes them the owner, and invites them by that name, so it has
// to be the email of a user and cannot be the admin.
package tenant

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// OrgsPath is the workspace the organizations live in.
	OrgsPath = "root:orgs"

	// OrganizationTimeout bounds the wait for an organization: its
	// workspace, authorization store, Keycloak realm and the invite of its
	// creator. AccountTimeout and InviteTimeout bound the other two.
	OrganizationTimeout = 5 * time.Minute
	AccountTimeout      = 3 * time.Minute
	InviteTimeout       = 3 * time.Minute

	tick = 2 * time.Second
)

// OrganizationPath is the workspace of the organization org.
func OrganizationPath(org string) string { return OrgsPath + ":" + org }

// AccountPath is the workspace of the account in the organization org.
func AccountPath(org, account string) string { return OrganizationPath(org) + ":" + account }

// Client is a client for the workspace at path, with the Platform Mesh core
// types registered.
func Client(t testing.TB, kcp *rest.Config, path string) ctrlruntimeclient.Client {
	t.Helper()
	cfg := rest.CopyConfig(kcp)
	cfg.Host = strings.TrimSuffix(cfg.Host, "/") + "/clusters/" + path
	if cfg.Timeout == 0 {
		cfg.Timeout = time.Minute
	}
	cl, err := ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: scheme.New(t, pmcorev1alpha1.AddToScheme)})
	require.NoError(t, err, "creating client for %s", cfg.Host)
	return cl
}

// As is the config of the user with the email, whom the admin impersonates.
// The user has no groups but system:authenticated, so they are allowed what
// Platform Mesh grants them: creating an organization, and whatever their
// roles in an organization allow.
func As(kcp *rest.Config, email string) *rest.Config {
	cfg := rest.CopyConfig(kcp)
	cfg.Impersonate = rest.ImpersonationConfig{UserName: email, Groups: []string{"system:authenticated"}}
	return cfg
}

// EnsureOrganization creates the organization name, owned by the user with
// the email creator, and waits until it is Ready. Platform Mesh then has
// created the Keycloak realm name and, in it, the user creator.
func EnsureOrganization(t testing.TB, kcp *rest.Config, name, creator string) *pmcorev1alpha1.Account {
	t.Helper()
	return ensureAccount(t, kcp, OrgsPath, pmcorev1alpha1.AccountTypeOrg, name, creator, OrganizationTimeout)
}

// EnsureAccount creates the account name in the organization org, owned by
// the user with the email creator, and waits until it is Ready.
func EnsureAccount(t testing.TB, kcp *rest.Config, org, name, creator string) *pmcorev1alpha1.Account {
	t.Helper()
	return ensureAccount(t, kcp, OrganizationPath(org), pmcorev1alpha1.AccountTypeAccount, name, creator, AccountTimeout)
}

// EnsureInvite creates the invite name for the user with the email in the
// organization org and waits until it is Ready: the user exists in the
// Keycloak realm org. The creator of an organization is invited by Platform
// Mesh, with an invite named like the organization.
func EnsureInvite(t testing.TB, kcp *rest.Config, org, name, email string) *pmcorev1alpha1.Invite {
	t.Helper()
	cl := Client(t, kcp, OrganizationPath(org))
	invite := &pmcorev1alpha1.Invite{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       pmcorev1alpha1.InviteSpec{Email: email},
	}
	if err := cl.Create(t.Context(), invite); !apierrors.IsAlreadyExists(err) {
		require.NoError(t, err, "creating invite %s in %s", name, OrganizationPath(org))
	}
	wait.Eventually(t, func() (bool, string) {
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(invite), invite); err != nil {
			return false, fmt.Sprintf("getting invite: %v", err)
		}
		return ready(invite.Status.Conditions)
	}, InviteTimeout, tick, "invite %s in %s should be Ready", name, OrganizationPath(org))
	require.Equal(t, email, invite.Spec.Email, "invite %s in %s exists for another user", name, OrganizationPath(org))
	return invite
}

// DeleteAccount deletes the account name of the organization org and waits
// until it is gone, with its workspace.
func DeleteAccount(t testing.TB, kcp *rest.Config, org, name string) {
	t.Helper()
	deleteAccount(t, kcp, OrganizationPath(org), name, AccountTimeout)
}

// DeleteOrganization deletes the organization name and waits until it is
// gone, with its workspace and its Keycloak realm.
func DeleteOrganization(t testing.TB, kcp *rest.Config, name string) {
	t.Helper()
	deleteAccount(t, kcp, OrgsPath, name, OrganizationTimeout)
}

func deleteAccount(t testing.TB, kcp *rest.Config, path, name string, timeout time.Duration) {
	t.Helper()
	cl := Client(t, kcp, path)
	account := &pmcorev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := cl.Delete(t.Context(), account); !apierrors.IsNotFound(err) {
		require.NoError(t, err, "deleting account %s in %s", name, path)
	}
	wait.Eventually(t, func() (bool, string) {
		err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(account), account)
		if apierrors.IsNotFound(err) {
			return true, ""
		}
		if err != nil {
			return false, fmt.Sprintf("getting account: %v", err)
		}
		_, conditions := ready(account.Status.Conditions)
		return false, fmt.Sprintf("still there, finalizers %v, %s", account.Finalizers, conditions)
	}, timeout, tick, "account %s in %s should be gone", name, path)
}

func ensureAccount(t testing.TB, kcp *rest.Config, path string, typ pmcorev1alpha1.AccountType, name, creator string, timeout time.Duration) *pmcorev1alpha1.Account {
	t.Helper()
	cl := Client(t, kcp, path)
	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		// spec.creator is set by the admission webhook, to the user who
		// sends the request.
		Spec: pmcorev1alpha1.AccountSpec{Type: typ, DisplayName: name},
	}
	if err := Client(t, As(kcp, creator), path).Create(t.Context(), account); !apierrors.IsAlreadyExists(err) {
		require.NoError(t, err, "creating %s %s in %s as %s", typ, name, path, creator)
	}
	wait.Eventually(t, func() (bool, string) {
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(account), account); err != nil {
			return false, fmt.Sprintf("getting account: %v", err)
		}
		return ready(account.Status.Conditions)
	}, timeout, tick, "%s %s should be Ready", typ, name)
	require.Equal(t, typ, account.Spec.Type, "account %s in %s exists with another type", name, path)
	require.Equal(t, &creator, account.Spec.Creator, "account %s in %s exists with another creator", name, path)
	return account
}

// ready reports whether the Ready condition is True, and the conditions
// otherwise.
func ready(conds []metav1.Condition) (bool, string) {
	if meta.IsStatusConditionTrue(conds, "Ready") {
		return true, ""
	}
	if len(conds) == 0 {
		return false, "status.conditions is empty"
	}
	lines := make([]string, 0, len(conds))
	for _, c := range conds {
		lines = append(lines, fmt.Sprintf("type=%s status=%s reason=%s message=%s", c.Type, c.Status, c.Reason, c.Message))
	}
	return false, "not Ready:\n" + strings.Join(lines, "\n")
}
