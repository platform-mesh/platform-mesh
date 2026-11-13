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
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"
)

// The tenant scenario: a tenant of the release has an organization with its
// owner, a second user invited to it, an account with an account nested in
// it, and data in the workspaces of both. After the upgrade all of it has
// to be there and be the same objects, not new ones alike; the owner has to
// get in the way a user does, by logging in, with kubectl and through the
// portal's gateway; and a stranger has to stay out.
//
// Then the tenant goes on: After adds to the organization the release
// created, changes its data and deletes from it.
//
// This file names what the scenario creates and puts its steps in order.
const (
	tenantOrg       = "upgrade-seed"
	tenantOwner     = "owner@upgrade-seed.example.com"
	tenantColleague = "colleague@upgrade-seed.example.com"
	tenantStranger  = "stranger@upgrade-seed.example.com"
	tenantInvite    = "colleague"
	tenantAccount   = "team-a"
	tenantNested    = "project-x"

	// What After adds.
	tenantNewAccount   = "team-b"
	tenantNewInvite    = "newcomer"
	tenantNewColleague = "newcomer@upgrade-seed.example.com"
)

// Tenant is the tenant scenario.
func Tenant() Scenario {
	var (
		org     = tenant.OrganizationPath(tenantOrg)
		account = tenant.AccountPath(tenantOrg, tenantAccount)
		nested  = account + ":" + tenantNested
	)
	// What must be the same objects after the upgrade.
	identity := []identity{
		accountIdentity(tenant.OrgsPath, tenantOrg),
		organizationIdentity(tenantOrg),
		accountIdentity(org, tenantAccount),
		accountIdentity(account, tenantNested),
		dataIdentity(account),
		dataIdentity(nested),
	}

	return Scenario{
		Name: "tenant",

		// ORDER MATTERS. Every step builds on the ones before it, and
		// a step starts only when the one before returned nil.
		Seed: []Step{
			// 1. First: the organization is the workspace, the Keycloak
			//    realm and the authorization store everything below
			//    lives in. Platform Mesh also creates the owner as a
			//    user of the realm while it gets Ready.
			{Name: "organization", Run: seedOrganization(tenantOrg, tenantOwner)},
			// 2. After the organization: the owner is only a user of
			//    the realm once the organization is Ready.
			{Name: "password of the owner", Run: seedPassword(tenantOrg, tenantOwner)},
			// 3. After the organization: the invite is an object in
			//    its workspace. It does not depend on the other steps.
			{Name: "invite", Run: seedInvite(tenantOrg, tenantInvite, tenantColleague)},
			// 4. After the organization: the account is an object in
			//    its workspace, created by the owner, who is only
			//    allowed to once the organization is Ready.
			{Name: "account", Run: seedAccount(org, tenantAccount, tenantOwner)},
			// 5. After the account: the nested account is an object in
			//    the workspace of the account.
			{Name: "nested account", Run: seedAccount(account, tenantNested, tenantOwner)},
			// 6. After their accounts: the data lives in their
			//    workspaces.
			{Name: "data in the account", Run: seedData(account)},
			{Name: "data in the nested account", Run: seedData(nested)},
			// 7. Last: it remembers the identities of all of the above.
			{Name: "identities", Run: seedIdentity(tenantOrg, identity...)},
		},

		// The validate steps only read, so their order changes no
		// result. They go from the outside in, so that the first step
		// to fail names the outermost thing that is wrong: a missing
		// organization fails "organization", not all of them.
		Validate: []Step{
			{Name: "organization", Run: validateOrganization(tenantOrg, tenantOwner)},
			{Name: "owner in the realm", Run: validateUser(tenantOrg, tenantOwner)},
			{Name: "invite of the owner", Run: validateInvite(tenantOrg, tenantOrg, tenantOwner)},
			{Name: "invite", Run: validateInvite(tenantOrg, tenantInvite, tenantColleague)},
			{Name: "invited user in the realm", Run: validateUserExists(tenantOrg, tenantColleague)},
			{Name: "account", Run: validateAccount(org, tenantAccount)},
			{Name: "nested account", Run: validateAccount(account, tenantNested)},
			{Name: "data in the account", Run: validateData(account)},
			{Name: "data in the nested account", Run: validateData(nested)},
			// After the steps above, which have shown that everything
			// is there: this one shows that it is what was seeded.
			{Name: "identities", Run: validateIdentity(tenantOrg, identity...)},
			// The authorization steps come last: they tell "not
			// allowed" from "not there" only because the steps above
			// have shown that the organization and its one account are
			// there. First by the roles alone, then the whole way of a
			// user: logged in, against kcp, the IAM service and the
			// gateway.
			{Name: "owner is allowed in", Run: validateAllowed(tenantOrg, tenantOwner, tenantAccount)},
			{Name: "stranger is kept out", Run: validateDenied(tenantOrg, tenantStranger)},
			{Name: "owner logs in", Run: validateLogin(tenantOrg, tenantOwner, tenantAccount)},
			{Name: "owner is known to the IAM service", Run: validateWhoAmI(tenantOrg, tenantOwner)},
			{Name: "owner reads through the gateway", Run: validateGateway(tenantOrg, tenantOwner, org, accounts, tenantAccount)},
		},

		// ORDER MATTERS little: each step works on what Seed left, not
		// on the step before it, except for the last three, which
		// belong together. What is deleted here, Seed creates again on
		// a kept cluster, and Validate holds again.
		After: []Step{
			// The data the release stored can be written.
			{Name: "change the data", Run: changeData(account)},
			// The organization the release created takes new users.
			{Name: "invite a newcomer", Run: seedInvite(tenantOrg, tenantNewInvite, tenantNewColleague)},
			{Name: "newcomer in the realm", Run: validateUserExists(tenantOrg, tenantNewColleague)},
			// An account the release created can be deleted: its
			// finalizers run on what the release wrote for it.
			{Name: "delete the nested account", Run: deleteAccount(account, tenantNested)},
			// The organization the release created takes new accounts,
			// and the owner, by the roles from the release, sees them.
			// The account is deleted again so that Validate, which
			// expects the one account of Seed, holds on a kept cluster.
			{Name: "second account", Run: seedAccount(org, tenantNewAccount, tenantOwner)},
			{Name: "owner sees both accounts", Run: validateLogin(tenantOrg, tenantOwner, tenantAccount, tenantNewAccount)},
			{Name: "delete the second account", Run: deleteAccount(org, tenantNewAccount)},
			// Last: the nested account is gone and Seed creates another,
			// so what Seed remembered no longer describes the scenario.
			{Name: "forget the identities", Run: forgetIdentity(tenantOrg)},
		},
	}
}
