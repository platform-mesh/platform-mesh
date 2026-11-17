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

package scenarios

import (
	"context"
	"errors"
	"fmt"
	"slices"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/keycloak"
	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Steps about an organization: the organization itself, the users of its
// realm, and who is allowed in.

// seedOrganization creates the organization for its owner.
func seedOrganization(org, owner string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kcp, err := env.KCP(ctx)
		if err != nil {
			return err
		}
		_, err = tenant.ApplyOrganization(ctx, kcp, org, owner)
		return err
	}
}

// seedPassword gives a user of the realm of the organization a password.
// Platform Mesh creates the users, the owner while the organization gets
// Ready and the others when they are invited, and such a user would have to
// set a password on the first login.
//
// It has to come after the step that makes Platform Mesh create the user:
// before that it would create a second, unrelated one.
func seedPassword(org, email string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kc, err := env.Keycloak(ctx)
		if err != nil {
			return err
		}
		_, err = kc.EnsureUser(ctx, org, keycloak.User{Email: email, FirstName: "Seed", LastName: "User", Password: password})
		return err
	}
}

// validateOrganization: the organization is Ready, belongs to its owner and
// has its workspace.
func validateOrganization(org, owner string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kcp, err := env.KCP(ctx)
		if err != nil {
			return err
		}
		account, err := tenant.GetOrganization(ctx, kcp, org)
		if err != nil {
			return err
		}
		if account.Spec.Creator == nil || *account.Spec.Creator != owner {
			return steps.Terminal("organization %s has creator %v, not %s", org, account.Spec.Creator, owner)
		}
		return workspaceReady(ctx, env, tenant.OrgsPath, org)
	}
}

// validateUser: the user is a user of the realm of the organization and can
// log in with the password of seedPassword, without further steps.
func validateUser(org, email string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kc, err := env.Keycloak(ctx)
		if err != nil {
			return err
		}
		user, err := kc.User(ctx, org, email)
		if err != nil {
			return err
		}
		if !user.Enabled || len(user.RequiredActions) != 0 {
			return steps.Terminal("%s cannot log in without further steps: enabled=%t, required actions %v", email, user.Enabled, user.RequiredActions)
		}
		return nil
	}
}

// validateUserExists: the user is a user of the realm of the organization.
// Whether they can log in is not asked: an invited user first has to set a
// password.
func validateUserExists(org, email string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kc, err := env.Keycloak(ctx)
		if err != nil {
			return err
		}
		_, err = kc.User(ctx, org, email)
		return err
	}
}

// validateNoUser: the realm of the organization has no user with the email.
func validateNoUser(org, email string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kc, err := env.Keycloak(ctx)
		if err != nil {
			return err
		}
		_, err = kc.User(ctx, org, email)
		switch {
		case err == nil:
			return steps.Terminal("%s is a user of the realm %s", email, org)
		case errors.Is(err, keycloak.ErrNotFound):
			return nil
		default:
			return err
		}
	}
}

// validateNoRealm: Keycloak has no realm of the organization, which is
// deleted with it. It is called again while the realm is there.
func validateNoRealm(org string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kc, err := env.Keycloak(ctx)
		if err != nil {
			return err
		}
		has, err := kc.HasRealm(ctx, org)
		if err != nil {
			return err
		}
		if has {
			return fmt.Errorf("the realm %s is still there", org)
		}
		return nil
	}
}

// validateLogin: the user logs in to the realm of the organization, and kcp
// takes the token and lets the user list the accounts of the organization,
// which are the ones named. This is the whole way of a request of a user:
// the realm, its client, the authentication of the workspace, and the
// authorization by the roles of the user.
func validateLogin(org, email string, accounts ...string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := userClient(ctx, env, org, email, tenant.OrganizationPath(org))
		if err != nil {
			return err
		}
		return sameAccounts(ctx, cl, fmt.Sprintf("%s, logged in", email), accounts)
	}
}

// validateAllowed: the roles of the user allow them to list the accounts of
// the organization, which are the ones named. It leaves the login out
// (impersonatingClient), so it also tells for a user without a password.
func validateAllowed(org, email string, accounts ...string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := impersonatingClient(ctx, env, tenant.OrganizationPath(org), email)
		if err != nil {
			return err
		}
		return sameAccounts(ctx, cl, email, accounts)
	}
}

// validateDenied: the user, who has no role in the organization, is not
// allowed to list its accounts.
func validateDenied(org, email string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := impersonatingClient(ctx, env, tenant.OrganizationPath(org), email)
		if err != nil {
			return err
		}
		return denied(cl.List(ctx, &pmcorev1alpha1.AccountList{}), fmt.Sprintf("%s listing the accounts of organization %s", email, org))
	}
}

// validateForeignToken: a user of the realm of the organization from logs
// in there, and the organization org does not take the token: kcp answers
// that it does not know who that is.
func validateForeignToken(from, email, org string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := userClient(ctx, env, from, email, tenant.OrganizationPath(org))
		if err != nil {
			return err
		}
		err = cl.List(ctx, &pmcorev1alpha1.AccountList{})
		what := fmt.Sprintf("%s of %s listing the accounts of organization %s", email, from, org)
		switch {
		case err == nil:
			return steps.Terminal("%s: allowed, but must not be", what)
		case apierrors.IsUnauthorized(err):
			return nil
		default:
			return denied(err, what)
		}
	}
}

// deleteOrganization deletes the organization and waits until it is gone,
// with its workspace.
func deleteOrganization(org string) stepFunc {
	return deleteAccount(tenant.OrgsPath, org)
}

// sameAccounts returns an error unless the accounts the client lists are
// the ones wanted.
func sameAccounts(ctx context.Context, cl ctrlruntimeclient.Client, who string, want []string) error {
	list := &pmcorev1alpha1.AccountList{}
	if err := cl.List(ctx, list); err != nil {
		return fmt.Errorf("listing the accounts as %s: %w", who, err)
	}
	got := make([]string, 0, len(list.Items))
	for _, account := range list.Items {
		got = append(got, account.Name)
	}
	slices.Sort(got)
	want = slices.Sorted(slices.Values(want))
	if !slices.Equal(got, want) {
		return steps.Terminal("%s sees the accounts %v, not %v", who, got, want)
	}
	return nil
}
