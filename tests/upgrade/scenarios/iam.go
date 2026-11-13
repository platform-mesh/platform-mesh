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
	"fmt"
	"maps"
	"slices"
	"strconv"

	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"
)

// Steps about roles, through the IAM service: the GraphQL API behind the
// members page of the portal. A role is granted on an account, by someone
// who may manage its roles, and ends up as tuples in the authorization
// store of the organization. Only the IAM service knows these tuples: no
// operator holds a copy it could write them from again.

const (
	iamPath = "/iam/graphql"

	roleOwner  = "owner"
	roleMember = "member"
)

// accountContext is the ResourceContext of the IAM service for the account
// in the organization.
func accountContext(org, account string) string {
	return fmt.Sprintf(`{group: "core.platform-mesh.io", kind: "Account", resource: {name: %s}, accountPath: %s}`,
		strconv.Quote(account), strconv.Quote(tenant.OrganizationPath(org)))
}

// rolesOf returns the roles the users of the account have, by email, as the
// user actor sees them.
func rolesOf(ctx context.Context, env Environment, org, account, actor string) (map[string][]string, error) {
	token, err := login(ctx, env, org, actor)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Users struct {
			Users []struct {
				User  struct{ Email string }
				Roles []struct{ ID string }
			}
		}
	}
	query := fmt.Sprintf(`{ users(context: %s, page: {limit: 100, page: 1}) { users { user { email } roles { id } } } }`, accountContext(org, account))
	if err := graphQL(ctx, env, org, iamPath, token, query, &answer); err != nil {
		return nil, fmt.Errorf("the users of account %s: %w", account, err)
	}
	roles := map[string][]string{}
	for _, u := range answer.Users.Users {
		for _, r := range u.Roles {
			roles[u.User.Email] = append(roles[u.User.Email], r.ID)
		}
		slices.Sort(roles[u.User.Email])
	}
	return roles, nil
}

// grantRole grants the user the role on the account, as the user actor,
// unless they have it.
func grantRole(org, account, actor, email, role string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		roles, err := rolesOf(ctx, env, org, account, actor)
		if err != nil {
			return err
		}
		if slices.Contains(roles[email], role) {
			return nil
		}
		token, err := login(ctx, env, org, actor)
		if err != nil {
			return err
		}
		var answer struct {
			AssignRolesToUsers struct {
				Success bool
				Errors  []string
			}
		}
		mutation := fmt.Sprintf(`mutation { assignRolesToUsers(context: %s, changes: [{userId: %s, roles: [%s]}]) { success errors } }`,
			accountContext(org, account), strconv.Quote(email), strconv.Quote(role))
		if err := graphQL(ctx, env, org, iamPath, token, mutation, &answer); err != nil {
			return fmt.Errorf("granting %s the role %s on account %s: %w", email, role, account, err)
		}
		if !answer.AssignRolesToUsers.Success {
			return fmt.Errorf("granting %s the role %s on account %s: %v", email, role, account, answer.AssignRolesToUsers.Errors)
		}
		return nil
	}
}

// revokeRole takes the role on the account from the user, as the user
// actor, unless they do not have it.
func revokeRole(org, account, actor, email, role string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		roles, err := rolesOf(ctx, env, org, account, actor)
		if err != nil {
			return err
		}
		if !slices.Contains(roles[email], role) {
			return nil
		}
		token, err := login(ctx, env, org, actor)
		if err != nil {
			return err
		}
		var answer struct {
			RemoveRole struct {
				Success bool
				Error   *string
			}
		}
		mutation := fmt.Sprintf(`mutation { removeRole(context: %s, input: {userId: %s, role: %s}) { success error } }`,
			accountContext(org, account), strconv.Quote(email), strconv.Quote(role))
		if err := graphQL(ctx, env, org, iamPath, token, mutation, &answer); err != nil {
			return fmt.Errorf("taking the role %s on account %s from %s: %w", role, account, email, err)
		}
		if !answer.RemoveRole.Success {
			message := "no reason given"
			if answer.RemoveRole.Error != nil {
				message = *answer.RemoveRole.Error
			}
			return fmt.Errorf("taking the role %s on account %s from %s: %s", role, account, email, message)
		}
		return nil
	}
}

// validateRoles: the IAM service reports, to the user actor, exactly the
// roles want for the account: the roles by the email of who has them.
func validateRoles(org, account, actor string, want map[string][]string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		got, err := rolesOf(ctx, env, org, account, actor)
		if err != nil {
			return err
		}
		if !maps.EqualFunc(got, want, slices.Equal[[]string]) {
			return steps.Terminal("account %s has the roles %v, not %v", account, got, want)
		}
		return nil
	}
}

// validateWhoAmI: the IAM service takes the token of the user and knows who
// they are.
func validateWhoAmI(org, email string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		token, err := login(ctx, env, org, email)
		if err != nil {
			return err
		}
		var answer struct{ Me struct{ Email string } }
		if err := graphQL(ctx, env, org, iamPath, token, `{ me { email } }`, &answer); err != nil {
			return err
		}
		if answer.Me.Email != email {
			return steps.Terminal("the IAM service takes %s for %q", email, answer.Me.Email)
		}
		return nil
	}
}
