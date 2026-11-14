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
// the portal does on behalf of a user, as the kcp admin.
//
//	kcp := pm.KCP(t, "")
//	tenant.EnsureOrganization(t, kcp, "acme", "owner@example.com")
//	tenant.EnsureAccount(t, kcp, "acme", "team-a", "owner@example.com")
//	tenant.EnsureInvite(t, kcp, "acme", "colleague", "colleague@example.com")
//
// A caller with a loop of its own (the steps of tests/upgrade) uses the
// functions that take a context, do one pass and return: Apply* creates what
// is missing, Get* never creates. Both return an error wrapping ErrNotReady
// until the object is Ready, so they are called again until they return nil,
// like a reconciler.
//
// Every function takes the kcp admin config without a workspace path and is
// idempotent. The users end up in the Keycloak realm named like the
// organization; see the keycloak package for giving them a password.
//
// Organizations and accounts are created as their creator, whom the admin
// impersonates: Platform Mesh records whoever sent the request as the
// creator, makes them the owner, and invites them by that name, so it has
// to be the email of a user and cannot be the admin.
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
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	// OrgsPath is the workspace the organizations live in.
	OrgsPath = "root:orgs"

	// OrganizationTimeout bounds the wait of the functions taking t for an
	// organization: its workspace, authorization store, Keycloak realm
	// and the invite of its creator. AccountTimeout and InviteTimeout
	// bound the other two.
	OrganizationTimeout = 5 * time.Minute
	AccountTimeout      = 3 * time.Minute
	InviteTimeout       = 3 * time.Minute

	tick = 2 * time.Second
)

// ErrNotReady is wrapped by the errors Apply* and Get* return for an object
// that exists but is not Ready yet: calling again later can succeed. It is
// wait.ErrNotReady.
var ErrNotReady = wait.ErrNotReady

// OrganizationPath is the workspace of the organization org.
func OrganizationPath(org string) string { return OrgsPath + ":" + org }

// AccountPath is the workspace of the account in the organization org.
func AccountPath(org, account string) string { return OrganizationPath(org) + ":" + account }

// As is the config of the user with the email, whom the admin impersonates.
// The user has no groups but system:authenticated, so they are allowed what
// Platform Mesh grants them: creating an organization, and whatever their
// roles in an organization allow.
func As(kcp *rest.Config, email string) *rest.Config {
	cfg := rest.CopyConfig(kcp)
	cfg.Impersonate = rest.ImpersonationConfig{UserName: email, Groups: []string{"system:authenticated"}}
	return cfg
}

// WithToken is the config of a user who logged in (keycloak.Client.Login):
// it carries the ID token of the user and nothing of the admin kcp belongs
// to, so kcp authenticates the request by the token alone.
func WithToken(kcp *rest.Config, token string) *rest.Config {
	return &rest.Config{
		Host:        kcp.Host,
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData:     kcp.CAData,
			CAFile:     kcp.CAFile,
			ServerName: kcp.ServerName,
			Insecure:   kcp.Insecure,
		},
	}
}

// KubectlClient is the public OIDC client every organization has for
// kubectl, and kubectlRedirect a redirect URI it accepts. Login logs in
// through it, like `kubectl oidc-login` does.
const (
	KubectlClient   = "kubectl"
	kubectlRedirect = "http://localhost:8000"

	// accountInfo is the name of the AccountInfo every workspace of an
	// organization or account has.
	accountInfo = "account"
)

// Login logs the user with the email in to the realm of the organization
// org, with the password, and returns their ID token: what kcp and the
// services of Platform Mesh take (see WithToken). It is the whole login of
// a user, through the OIDC client the organization names in its
// AccountInfo; the user has to be able to log in without further steps
// (keycloak.Client.EnsureUser).
func Login(ctx context.Context, kcp *rest.Config, kc keycloak.Client, org, email, password string) (string, error) {
	admin, err := NewClient(kcp, OrganizationPath(org))
	if err != nil {
		return "", err
	}
	info := &pmcorev1alpha1.AccountInfo{}
	if err := admin.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountInfo}, info); err != nil {
		return "", fmt.Errorf("the AccountInfo of organization %s: %w", org, err)
	}
	if info.Spec.OIDC == nil || info.Spec.OIDC.Clients[KubectlClient].ClientID == "" {
		return "", fmt.Errorf("the AccountInfo of organization %s names no %s client", org, KubectlClient)
	}
	tokens, err := kc.Login(ctx, org, info.Spec.OIDC.Clients[KubectlClient].ClientID, kubectlRedirect, email, password)
	if err != nil {
		return "", err
	}
	return tokens.IDToken, nil
}

// NewClient returns a client for the workspace at path, with the scheme of
// the framework (scheme.Platform). Clients are kept and handed out again:
// see pm.NewClient.
func NewClient(kcp *rest.Config, path string) (ctrlruntimeclient.Client, error) {
	return pm.NewClient(pm.Workspace(kcp, path))
}

// Client is NewClient for a test.
func Client(t testing.TB, kcp *rest.Config, path string) ctrlruntimeclient.Client {
	t.Helper()
	cl, err := NewClient(kcp, path)
	require.NoError(t, err)
	return cl
}

// --- one pass, for callers with a loop of their own -------------------------

// ApplyOrganization creates the organization name, owned by the user with
// the email creator, unless it exists. It returns an error wrapping
// ErrNotReady until the organization is Ready: Platform Mesh then has
// created the Keycloak realm name and, in it, the user creator.
func ApplyOrganization(ctx context.Context, kcp *rest.Config, name, creator string) (*pmcorev1alpha1.Account, error) {
	return applyAccount(ctx, kcp, OrgsPath, pmcorev1alpha1.AccountTypeOrg, name, creator)
}

// ApplyAccount creates the account name in the organization org, owned by
// the user with the email creator, unless it exists. It returns an error
// wrapping ErrNotReady until the account is Ready.
func ApplyAccount(ctx context.Context, kcp *rest.Config, org, name, creator string) (*pmcorev1alpha1.Account, error) {
	return applyAccount(ctx, kcp, OrganizationPath(org), pmcorev1alpha1.AccountTypeAccount, name, creator)
}

// ApplyAccountIn is ApplyAccount for an account in the workspace at parent,
// which is the workspace of an organization or, for a nested account, of
// another account.
func ApplyAccountIn(ctx context.Context, kcp *rest.Config, parent, name, creator string) (*pmcorev1alpha1.Account, error) {
	return applyAccount(ctx, kcp, parent, pmcorev1alpha1.AccountTypeAccount, name, creator)
}

// ApplyInvite creates the invite name for the user with the email in the
// organization org unless it exists. It returns an error wrapping
// ErrNotReady until the invite is Ready: the user exists in the Keycloak
// realm org. The creator of an organization is invited by Platform Mesh,
// with an invite named like the organization.
//
//nolint:staticcheck // the deprecated kind must keep working for existing integrations
func ApplyInvite(ctx context.Context, kcp *rest.Config, org, name, email string) (*pmcorev1alpha1.Invite, error) {
	path := OrganizationPath(org)
	cl, err := NewClient(kcp, path)
	if err != nil {
		return nil, err
	}
	invite := &pmcorev1alpha1.Invite{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       pmcorev1alpha1.InviteSpec{Email: email},
	}
	if err := cl.Create(ctx, invite); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("creating invite %s in %s: %w", name, path, err)
	}
	invite, err = getInvite(ctx, cl, path, name)
	if invite != nil && invite.Spec.Email != email {
		return nil, fmt.Errorf("invite %s in %s exists for another user: %s", name, path, invite.Spec.Email)
	}
	return invite, err
}

// GetOrganization returns the organization name. It creates nothing: the
// error is the NotFound of the API when there is none, and wraps ErrNotReady
// until the organization is Ready.
func GetOrganization(ctx context.Context, kcp *rest.Config, name string) (*pmcorev1alpha1.Account, error) {
	return getAccountIn(ctx, kcp, OrgsPath, pmcorev1alpha1.AccountTypeOrg, name)
}

// GetAccount returns the account name of the organization org, like
// GetOrganization.
func GetAccount(ctx context.Context, kcp *rest.Config, org, name string) (*pmcorev1alpha1.Account, error) {
	return getAccountIn(ctx, kcp, OrganizationPath(org), pmcorev1alpha1.AccountTypeAccount, name)
}

// GetAccountIn is GetAccount for an account in the workspace at parent, like
// ApplyAccountIn.
func GetAccountIn(ctx context.Context, kcp *rest.Config, parent, name string) (*pmcorev1alpha1.Account, error) {
	return getAccountIn(ctx, kcp, parent, pmcorev1alpha1.AccountTypeAccount, name)
}

// GetInvite returns the invite name of the organization org, like
// GetOrganization.
//
//nolint:staticcheck // the deprecated kind must keep working for existing integrations
func GetInvite(ctx context.Context, kcp *rest.Config, org, name string) (*pmcorev1alpha1.Invite, error) {
	path := OrganizationPath(org)
	cl, err := NewClient(kcp, path)
	if err != nil {
		return nil, err
	}
	return getInvite(ctx, cl, path, name)
}

func applyAccount(ctx context.Context, kcp *rest.Config, path string, typ pmcorev1alpha1.AccountType, name, creator string) (*pmcorev1alpha1.Account, error) {
	as, err := NewClient(As(kcp, creator), path)
	if err != nil {
		return nil, err
	}
	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		// spec.creator is set by the admission webhook, to the user who
		// sends the request.
		Spec: pmcorev1alpha1.AccountSpec{Type: typ, DisplayName: name},
	}
	if err := as.Create(ctx, account); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("creating %s %s in %s as %s: %w", typ, name, path, creator, err)
	}
	account, err = getAccountIn(ctx, kcp, path, typ, name)
	if account != nil && (account.Spec.Creator == nil || *account.Spec.Creator != creator) {
		return nil, fmt.Errorf("account %s in %s exists with another creator: %v", name, path, account.Spec.Creator)
	}
	return account, err
}

// getAccountIn returns the account also when it is not Ready, with the
// error saying so.
func getAccountIn(ctx context.Context, kcp *rest.Config, path string, typ pmcorev1alpha1.AccountType, name string) (*pmcorev1alpha1.Account, error) {
	cl, err := NewClient(kcp, path)
	if err != nil {
		return nil, err
	}
	account := &pmcorev1alpha1.Account{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, account); err != nil {
		return nil, fmt.Errorf("account %s in %s: %w", name, path, err)
	}
	if account.Spec.Type != typ {
		return nil, fmt.Errorf("account %s in %s has type %s, not %s", name, path, account.Spec.Type, typ)
	}
	if err := wait.CheckReady(account.Status.Conditions); err != nil {
		return account, fmt.Errorf("account %s in %s: %w", name, path, err)
	}
	return account, nil
}

//nolint:staticcheck // the deprecated kind must keep working for existing integrations
func getInvite(ctx context.Context, cl ctrlruntimeclient.Client, path, name string) (*pmcorev1alpha1.Invite, error) {
	invite := &pmcorev1alpha1.Invite{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, invite); err != nil {
		return nil, fmt.Errorf("invite %s in %s: %w", name, path, err)
	}
	if err := wait.CheckReady(invite.Status.Conditions); err != nil {
		return invite, fmt.Errorf("invite %s in %s: %w", name, path, err)
	}
	return invite, nil
}

// --- for tests: wait until Ready ---------------------------------------------

// EnsureOrganization is ApplyOrganization for a test: it waits until the
// organization is Ready.
func EnsureOrganization(t testing.TB, kcp *rest.Config, name, creator string) *pmcorev1alpha1.Account {
	t.Helper()
	return eventually(t, OrganizationTimeout, "organization "+name, func() (*pmcorev1alpha1.Account, error) {
		return ApplyOrganization(t.Context(), kcp, name, creator)
	})
}

// EnsureAccount is ApplyAccount for a test: it waits until the account is
// Ready.
func EnsureAccount(t testing.TB, kcp *rest.Config, org, name, creator string) *pmcorev1alpha1.Account {
	t.Helper()
	return eventually(t, AccountTimeout, "account "+name+" in "+OrganizationPath(org), func() (*pmcorev1alpha1.Account, error) {
		return ApplyAccount(t.Context(), kcp, org, name, creator)
	})
}

// EnsureInvite is ApplyInvite for a test: it waits until the invite is
// Ready.
//
//nolint:staticcheck // the deprecated kind must keep working for existing integrations
func EnsureInvite(t testing.TB, kcp *rest.Config, org, name, email string) *pmcorev1alpha1.Invite {
	t.Helper()
	return eventually(t, InviteTimeout, "invite "+name+" in "+OrganizationPath(org), func() (*pmcorev1alpha1.Invite, error) {
		return ApplyInvite(t.Context(), kcp, org, name, email)
	})
}

// RequireOrganization is GetOrganization for a test: it fails t unless the
// organization exists, and waits until it is Ready.
func RequireOrganization(t testing.TB, kcp *rest.Config, name string) *pmcorev1alpha1.Account {
	t.Helper()
	return eventually(t, OrganizationTimeout, "organization "+name, func() (*pmcorev1alpha1.Account, error) {
		return GetOrganization(t.Context(), kcp, name)
	})
}

// RequireAccount is GetAccount for a test: it fails t unless the account
// exists, and waits until it is Ready.
func RequireAccount(t testing.TB, kcp *rest.Config, org, name string) *pmcorev1alpha1.Account {
	t.Helper()
	return eventually(t, AccountTimeout, "account "+name+" in "+OrganizationPath(org), func() (*pmcorev1alpha1.Account, error) {
		return GetAccount(t.Context(), kcp, org, name)
	})
}

// RequireInvite is GetInvite for a test: it fails t unless the invite
// exists, and waits until it is Ready.
//
//nolint:staticcheck // the deprecated kind must keep working for existing integrations
func RequireInvite(t testing.TB, kcp *rest.Config, org, name string) *pmcorev1alpha1.Invite {
	t.Helper()
	return eventually(t, InviteTimeout, "invite "+name+" in "+OrganizationPath(org), func() (*pmcorev1alpha1.Invite, error) {
		return GetInvite(t.Context(), kcp, org, name)
	})
}

// eventually waits for get to return an object that is Ready (wait.For).
func eventually[T any](t testing.TB, timeout time.Duration, what string, get func() (T, error)) T {
	t.Helper()
	return wait.For(t, timeout, tick, what+" should be Ready", get)
}

// DeleteAccountIn deletes the Account name in the workspace at parent, an
// organization, an account or, for an organization, OrgsPath. It is one
// pass: it returns an error until the account is gone with its workspace,
// so it is called again until it returns nil.
func DeleteAccountIn(ctx context.Context, kcp *rest.Config, parent, name string) error {
	cl, err := NewClient(kcp, parent)
	if err != nil {
		return err
	}
	account := &pmcorev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := ctrlruntimeclient.IgnoreNotFound(cl.Delete(ctx, account)); err != nil {
		return fmt.Errorf("deleting account %s in %s: %w", name, parent, err)
	}
	key := ctrlruntimeclient.ObjectKey{Name: name}
	err = cl.Get(ctx, key, account)
	if err == nil {
		return fmt.Errorf("account %s in %s is still there, finalizers %v, %v", name, parent, account.Finalizers, wait.CheckReady(account.Status.Conditions))
	}
	if err := wait.CheckGone(err, fmt.Sprintf("account %s in %s", name, parent)); err != nil {
		return err
	}
	return wait.CheckGone(cl.Get(ctx, key, &kcptenancyv1alpha1.Workspace{}), fmt.Sprintf("workspace %s in %s", name, parent))
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
	wait.Eventually(t, func() (bool, string) {
		if err := DeleteAccountIn(t.Context(), kcp, path, name); err != nil {
			return false, err.Error()
		}
		return true, ""
	}, timeout, tick, "account %s in %s should be gone", name, path)
}
