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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
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
// that exists but is not Ready yet: calling again later can succeed.
var ErrNotReady = errors.New("not ready")

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

// scheme has client-go's built-in types, kcp's Workspace and the Platform
// Mesh core types registered.
var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
}

// NewClient returns a client for the workspace at path, with client-go's
// built-in types, kcp's Workspace and the Platform Mesh core types
// registered.
func NewClient(kcp *rest.Config, path string) (ctrlruntimeclient.Client, error) {
	cfg := rest.CopyConfig(kcp)
	cfg.Host = strings.TrimSuffix(cfg.Host, "/") + "/clusters/" + path
	if cfg.Timeout == 0 {
		cfg.Timeout = time.Minute
	}
	cl, err := ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating client for %s: %w", cfg.Host, err)
	}
	return cl, nil
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

// ApplyInvite creates the invite name for the user with the email in the
// organization org unless it exists. It returns an error wrapping
// ErrNotReady until the invite is Ready: the user exists in the Keycloak
// realm org. The creator of an organization is invited by Platform Mesh,
// with an invite named like the organization.
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

// GetInvite returns the invite name of the organization org, like
// GetOrganization.
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
	if err := notReady(account.Status.Conditions); err != nil {
		return account, fmt.Errorf("account %s in %s: %w", name, path, err)
	}
	return account, nil
}

func getInvite(ctx context.Context, cl ctrlruntimeclient.Client, path, name string) (*pmcorev1alpha1.Invite, error) {
	invite := &pmcorev1alpha1.Invite{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, invite); err != nil {
		return nil, fmt.Errorf("invite %s in %s: %w", name, path, err)
	}
	if err := notReady(invite.Status.Conditions); err != nil {
		return invite, fmt.Errorf("invite %s in %s: %w", name, path, err)
	}
	return invite, nil
}

// notReady is nil when the Ready condition is True, and otherwise an error
// wrapping ErrNotReady that lists the conditions.
func notReady(conds []metav1.Condition) error {
	if meta.IsStatusConditionTrue(conds, "Ready") {
		return nil
	}
	if len(conds) == 0 {
		return fmt.Errorf("%w: status.conditions is empty", ErrNotReady)
	}
	lines := make([]string, 0, len(conds))
	for _, c := range conds {
		lines = append(lines, fmt.Sprintf("type=%s status=%s reason=%s message=%s", c.Type, c.Status, c.Reason, c.Message))
	}
	return fmt.Errorf("%w:\n%s", ErrNotReady, strings.Join(lines, "\n"))
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
func RequireInvite(t testing.TB, kcp *rest.Config, org, name string) *pmcorev1alpha1.Invite {
	t.Helper()
	return eventually(t, InviteTimeout, "invite "+name+" in "+OrganizationPath(org), func() (*pmcorev1alpha1.Invite, error) {
		return GetInvite(t.Context(), kcp, org, name)
	})
}

// eventually calls get until it returns no error. Only ErrNotReady is
// waited for; any other error fails t at once.
func eventually[T any](t testing.TB, timeout time.Duration, what string, get func() (T, error)) T {
	t.Helper()
	var obj T
	wait.Eventually(t, func() (bool, string) {
		var err error
		obj, err = get()
		if err == nil {
			return true, ""
		}
		if !errors.Is(err, ErrNotReady) {
			require.NoError(t, err)
		}
		return false, err.Error()
	}, timeout, tick, "%s should be Ready", what)
	return obj
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
		return false, fmt.Sprintf("still there, finalizers %v, %v", account.Finalizers, notReady(account.Status.Conditions))
	}, timeout, tick, "account %s in %s should be gone", name, path)
}
