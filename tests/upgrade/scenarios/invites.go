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

// The steps of the tenant scenario (tenant.go) about the
// invites to the organization and the users they create.

// seedInvite invites a second user to the organization.
func seedInvite(ctx context.Context, env Environment) error {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return err
	}
	_, err = tenant.ApplyInvite(ctx, kcp, tenantOrg, tenantInvite, tenantColleague)
	return err
}

// validateInvites: both invites are Ready and for the right user, the one
// seedInvite created and the one Platform Mesh created for the owner, which
// it names like the organization.
func validateInvites(ctx context.Context, env Environment) error {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return err
	}
	for name, email := range map[string]string{tenantOrg: tenantOwner, tenantInvite: tenantColleague} {
		invite, err := tenant.GetInvite(ctx, kcp, tenantOrg, name)
		if err != nil {
			return err
		}
		if invite.Spec.Email != email {
			return steps.Terminal("invite %s is for %s, not %s", name, invite.Spec.Email, email)
		}
	}
	return nil
}

// validateInvitedUser: the invited user is a user of the realm of the
// organization.
func validateInvitedUser(ctx context.Context, env Environment) error {
	kc, err := env.Keycloak(ctx)
	if err != nil {
		return err
	}
	_, err = kc.User(ctx, tenantOrg, tenantColleague)
	return err
}
