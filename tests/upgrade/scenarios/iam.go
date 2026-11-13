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
	"maps"
	"slices"

	"go.platform-mesh.io/platform-mesh/tests/framework/iam"
	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
)

// Steps about roles, through the IAM service (tests/framework/iam): the API
// behind the members page of the portal. A role is granted on an account,
// by a user who may manage its roles, here always called the actor.

// grantRole grants the user the role on the account, as the user actor,
// unless they have it.
func grantRole(org, account, actor, email, role string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		token, err := login(ctx, env, org, actor)
		if err != nil {
			return err
		}
		return iam.Grant(ctx, env, org, account, token, email, role)
	}
}

// revokeRole takes the role on the account from the user, as the user
// actor, unless they do not have it.
func revokeRole(org, account, actor, email, role string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		token, err := login(ctx, env, org, actor)
		if err != nil {
			return err
		}
		return iam.Revoke(ctx, env, org, account, token, email, role)
	}
}

// validateRoles: the IAM service reports, to the user actor, exactly the
// roles want for the account: the roles by the email of who has them.
func validateRoles(org, account, actor string, want map[string][]string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		token, err := login(ctx, env, org, actor)
		if err != nil {
			return err
		}
		got, err := iam.Roles(ctx, env, org, account, token)
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
		got, err := iam.WhoAmI(ctx, env, org, token)
		if err != nil {
			return err
		}
		if got != email {
			return steps.Terminal("the IAM service takes %s for %q", email, got)
		}
		return nil
	}
}
