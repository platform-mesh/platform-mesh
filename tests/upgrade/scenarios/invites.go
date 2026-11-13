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

	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"
)

// Steps about the invites to an organization. An invite makes its user a
// user of the realm of the organization; validateUser and validateLogin
// (organization.go) check on that.

// seedInvite invites a user to the organization.
func seedInvite(org, name, email string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kcp, err := env.KCP(ctx)
		if err != nil {
			return err
		}
		_, err = tenant.ApplyInvite(ctx, kcp, org, name, email)
		return err
	}
}

// validateInvite: the invite is Ready and for the user. Platform Mesh
// invites the owner of an organization itself, with an invite named like
// the organization.
func validateInvite(org, name, email string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kcp, err := env.KCP(ctx)
		if err != nil {
			return err
		}
		invite, err := tenant.GetInvite(ctx, kcp, org, name)
		if err != nil {
			return err
		}
		if invite.Spec.Email != email {
			return steps.Terminal("invite %s is for %s, not %s", name, invite.Spec.Email, email)
		}
		return nil
	}
}
