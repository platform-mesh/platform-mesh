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

// The isolation scenario: two tenants of the release, each with an
// organization of its own. After the upgrade each owner gets into their own
// organization and not into the other one: not by their roles, and not with
// their token, which is of another realm. Neither is a user of the realm of
// the other.
//
// A change to the authorization model, or to how the stores and realms of
// organizations are told apart, can open one tenant to another while every
// tenant on its own still looks right.
const (
	isolationOrgA   = "upgrade-iso-a"
	isolationOwnerA = "owner@upgrade-iso-a.example.com"
	isolationOrgB   = "upgrade-iso-b"
	isolationOwnerB = "owner@upgrade-iso-b.example.com"
	// Both organizations have an account of this name: the same name in
	// two organizations must not mix either.
	isolationAccount = "main"
)

// Isolation is the isolation scenario.
func Isolation() Scenario {
	s := Scenario{Name: "isolation"}
	tenants := []struct{ org, owner, other, stranger string }{
		{isolationOrgA, isolationOwnerA, isolationOrgB, isolationOwnerB},
		{isolationOrgB, isolationOwnerB, isolationOrgA, isolationOwnerA},
	}

	// ORDER MATTERS within a tenant: the organization, then its owner's
	// password and its account, as in the tenant scenario. The two tenants
	// do not depend on each other.
	for _, t := range tenants {
		s.Seed = append(s.Seed,
			Step{Name: "organization " + t.org, Run: seedOrganization(t.org, t.owner)},
			Step{Name: "password of the owner of " + t.org, Run: seedPassword(t.org, t.owner)},
			Step{Name: "account in " + t.org, Run: seedAccount(tenant.OrganizationPath(t.org), isolationAccount, t.owner)},
		)
	}

	// First that both tenants are there and their owners get in, then
	// that they are apart: "kept out" says little about an organization
	// nobody gets into.
	for _, t := range tenants {
		s.Validate = append(s.Validate,
			Step{Name: "organization " + t.org, Run: validateOrganization(t.org, t.owner)},
			Step{Name: "account in " + t.org, Run: validateAccount(tenant.OrganizationPath(t.org), isolationAccount)},
			Step{Name: "owner logs in to " + t.org, Run: validateLogin(t.org, t.owner, isolationAccount)},
		)
	}
	for _, t := range tenants {
		s.Validate = append(s.Validate,
			Step{Name: "owner of " + t.other + " has no role in " + t.org, Run: validateDenied(t.org, t.stranger)},
			Step{Name: "token of " + t.other + " is not taken by " + t.org, Run: validateForeignToken(t.other, t.stranger, t.org)},
			Step{Name: "owner of " + t.other + " is no user of " + t.org, Run: validateNoUser(t.org, t.stranger)},
		)
	}
	return s
}
