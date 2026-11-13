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

// The offboarding scenario: a tenant of the release leaves after the
// upgrade. Its organization, with an invited user, an account and data, is
// there after the upgrade like any other, and then After deletes the
// account and the organization.
//
// Deleting is where the upgraded operators meet most of what the release
// wrote: every finalizer on the Account, its workspace, its AccountInfo and
// the store and realm of the organization has to find what it cleans up
// the way the release left it. A finalizer that does not, blocks forever,
// and the steps time out on an object that is "still there".
//
// The other scenarios keep their organizations, for a look at a kept
// cluster and for the next run on it; this one is here to lose its own.
const (
	offboardingOrg       = "upgrade-leaving"
	offboardingOwner     = "owner@upgrade-leaving.example.com"
	offboardingColleague = "colleague@upgrade-leaving.example.com"
	offboardingAccount   = "legacy"
)

// Offboarding is the offboarding scenario.
func Offboarding() Scenario {
	var (
		org     = tenant.OrganizationPath(offboardingOrg)
		account = tenant.AccountPath(offboardingOrg, offboardingAccount)
	)
	return Scenario{
		Name: "offboarding",

		// ORDER MATTERS, as in the tenant scenario: the organization,
		// then what is in it, then what is in its account.
		Seed: []Step{
			{Name: "organization", Run: seedOrganization(offboardingOrg, offboardingOwner)},
			{Name: "invite", Run: seedInvite(offboardingOrg, "colleague", offboardingColleague)},
			{Name: "account", Run: seedAccount(org, offboardingAccount, offboardingOwner)},
			{Name: "data in the account", Run: seedData(account)},
		},

		Validate: []Step{
			{Name: "organization", Run: validateOrganization(offboardingOrg, offboardingOwner)},
			{Name: "invite", Run: validateInvite(offboardingOrg, "colleague", offboardingColleague)},
			{Name: "account", Run: validateAccount(org, offboardingAccount)},
			{Name: "data in the account", Run: validateData(account)},
			{Name: "owner is allowed in", Run: validateAllowed(offboardingOrg, offboardingOwner, offboardingAccount)},
		},

		// ORDER MATTERS: the account before the organization it is in.
		// Deleting the organization alone would take the account along;
		// this way a finalizer that hangs names which of the two it is
		// on.
		After: []Step{
			{Name: "delete the account", Run: deleteAccount(org, offboardingAccount)},
			{Name: "delete the organization", Run: deleteOrganization(offboardingOrg)},
			{Name: "realm of the organization is gone", Run: validateNoRealm(offboardingOrg)},
		},
	}
}
