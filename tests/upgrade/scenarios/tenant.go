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

	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"

	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// The tenant scenario: a tenant of the release has an organization with its
// owner, a second user invited to it, an account, and an object in the
// workspace of the account. After the upgrade they have to be there, the
// users have to be in the realm of the organization, and the owner has to be
// allowed into the organization while a stranger is not.
//
// This file names what the scenario creates and puts its steps in order. The
// steps themselves are in organization.go, invites.go and
// account.go.
const (
	tenantOrg       = "upgrade-seed"
	tenantAccount   = "team-a"
	tenantOwner     = "owner@upgrade-seed.example.com"
	tenantColleague = "colleague@upgrade-seed.example.com"
	tenantStranger  = "stranger@upgrade-seed.example.com"
	tenantInvite    = "colleague"
	tenantPassword  = "MyPass1234"
	tenantConfigMap = "upgrade-seed"
)

// Tenant is the tenant scenario. Its steps are listed here, in one place,
// because their order matters; the steps themselves are in organization.go,
// invites.go and account.go.
func Tenant() Scenario {
	return Scenario{
		Name: "tenant",

		// ORDER MATTERS. Every step builds on the ones before it, and
		// a step starts only when the one before returned
		// nil.
		Seed: []Step{
			// 1. First: the organization is the workspace, the Keycloak
			//    realm and the authorization store everything below
			//    lives in. Platform Mesh also creates the owner as a
			//    user of the realm while it gets Ready.
			{Name: "organization", Run: seedOrganization},
			// 2. After the organization: the owner is only a user of
			//    the realm once the organization is Ready; before
			//    that this step would create a second, unrelated one.
			{Name: "password of the owner", Run: seedOwnerPassword},
			// 3. After the organization: the invite is an object in
			//    its workspace. It does not depend on steps 2, 4, 5.
			{Name: "invite", Run: seedInvite},
			// 4. After the organization: the account is an object in
			//    its workspace, created by the owner, who is only
			//    allowed to once the organization is Ready.
			{Name: "account", Run: seedAccount},
			// 5. After the account: the ConfigMap lives in the
			//    workspace of the account.
			{Name: "configmap in the account", Run: seedAccountConfigMap},
		},

		// The validate steps only read, so their order changes no
		// result. They go from the outside in, so that the first step
		// to fail names the outermost thing that is wrong: a missing
		// organization fails "organization", not seven steps.
		Validate: []Step{
			{Name: "organization", Run: validateOrganization},
			{Name: "owner in the realm", Run: validateOwnerUser},
			{Name: "invites", Run: validateInvites},
			{Name: "invited user in the realm", Run: validateInvitedUser},
			{Name: "account", Run: validateAccount},
			{Name: "configmap in the account", Run: validateAccountConfigMap},
			// The two authorization steps come last: they tell
			// "not allowed" from "not there" only because the steps
			// above have shown that the organization and its one
			// account are there.
			{Name: "owner is allowed in", Run: validateOwnerAllowed},
			{Name: "stranger is kept out", Run: validateStrangerDenied},
		},
	}
}

// tenantClient is a client for the workspace at path, as the kcp admin or,
// with as, as that user.
func tenantClient(ctx context.Context, env Environment, path, as string) (ctrlruntimeclient.Client, error) {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return nil, err
	}
	if as != "" {
		kcp = tenant.As(kcp, as)
	}
	return tenant.NewClient(kcp, path)
}

// workspaceReady returns an error unless the Workspace name in the
// workspace at path is Ready.
func workspaceReady(ctx context.Context, kcp *rest.Config, path, name string) error {
	cl, err := tenant.NewClient(kcp, path)
	if err != nil {
		return err
	}
	ws := &kcptenancyv1alpha1.Workspace{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, ws); err != nil {
		return err
	}
	if ws.Status.Phase != "Ready" {
		return fmt.Errorf("workspace %s in %s is %s", name, path, ws.Status.Phase)
	}
	return nil
}
