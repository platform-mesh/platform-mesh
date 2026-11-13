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
// Package iam talks to the IAM service of a deployed Platform Mesh: the
// GraphQL API behind the members page of the portal. A role is granted on
// an account, by a user who may manage its roles, and ends up as tuples in
// the authorization store of the organization. Only the IAM service knows
// these tuples: no operator holds a copy it could write them from again.
//
// Every function acts as a user, by the ID token of their login
// (tenant.Login).
package iam

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"go.platform-mesh.io/platform-mesh/tests/framework/graphql"
	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"
)

const (
	// path is where the IAM service of an organization is served.
	path = "/iam/graphql"

	// The roles of an account.
	RoleOwner  = "owner"
	RoleMember = "member"
)

// accountContext is the ResourceContext of the IAM service for the account
// in the organization.
func accountContext(org, account string) string {
	return fmt.Sprintf(`{group: "core.platform-mesh.io", kind: "Account", resource: {name: %s}, accountPath: %s}`,
		strconv.Quote(account), strconv.Quote(tenant.OrganizationPath(org)))
}

// WhoAmI returns the email the IAM service of the organization takes the
// user with the token for.
func WhoAmI(ctx context.Context, inst pm.Installation, org, token string) (string, error) {
	var answer struct{ Me struct{ Email string } }
	if err := graphql.Query(ctx, inst, org, path, token, `{ me { email } }`, &answer); err != nil {
		return "", err
	}
	return answer.Me.Email, nil
}

// Roles returns the roles the users of the account have, sorted, by the
// email of the user.
func Roles(ctx context.Context, inst pm.Installation, org, account, token string) (map[string][]string, error) {
	var answer struct {
		Users struct {
			Users []struct {
				User  struct{ Email string }
				Roles []struct{ ID string }
			}
		}
	}
	query := fmt.Sprintf(`{ users(context: %s, page: {limit: 100, page: 1}) { users { user { email } roles { id } } } }`, accountContext(org, account))
	if err := graphql.Query(ctx, inst, org, path, token, query, &answer); err != nil {
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

// Grant grants the user with the email the role on the account, unless they
// have it.
func Grant(ctx context.Context, inst pm.Installation, org, account, token, email, role string) error {
	roles, err := Roles(ctx, inst, org, account, token)
	if err != nil {
		return err
	}
	if slices.Contains(roles[email], role) {
		return nil
	}
	var answer struct {
		AssignRolesToUsers struct {
			Success bool
			Errors  []string
		}
	}
	mutation := fmt.Sprintf(`mutation { assignRolesToUsers(context: %s, changes: [{userId: %s, roles: [%s]}]) { success errors } }`,
		accountContext(org, account), strconv.Quote(email), strconv.Quote(role))
	if err := graphql.Query(ctx, inst, org, path, token, mutation, &answer); err != nil {
		return fmt.Errorf("granting %s the role %s on account %s: %w", email, role, account, err)
	}
	if !answer.AssignRolesToUsers.Success {
		return fmt.Errorf("granting %s the role %s on account %s: %v", email, role, account, answer.AssignRolesToUsers.Errors)
	}
	return nil
}

// Revoke takes the role on the account from the user with the email, unless
// they do not have it.
func Revoke(ctx context.Context, inst pm.Installation, org, account, token, email, role string) error {
	roles, err := Roles(ctx, inst, org, account, token)
	if err != nil {
		return err
	}
	if !slices.Contains(roles[email], role) {
		return nil
	}
	var answer struct {
		RemoveRole struct {
			Success bool
			Error   *string
		}
	}
	mutation := fmt.Sprintf(`mutation { removeRole(context: %s, input: {userId: %s, role: %s}) { success error } }`,
		accountContext(org, account), strconv.Quote(email), strconv.Quote(role))
	if err := graphql.Query(ctx, inst, org, path, token, mutation, &answer); err != nil {
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
