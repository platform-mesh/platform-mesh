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

	"go.platform-mesh.io/platform-mesh/tests/framework/iam"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The roles scenario: the owner of an organization has made a colleague a
// member of an account, on the members page of the portal, that is through
// the IAM service. Another colleague was invited and got no role. After the
// upgrade the member still gets into the account and the guest still does
// not, each logged in as themselves.
//
// The roles are tuples in the authorization store of the organization that
// only the IAM service wrote; an upgrade that loses or re-keys the store
// loses them, and nothing puts them back.
//
// Then, After: the owner takes the role from the member and gives it to the
// guest, on the store the release filled, and kcp follows; and then puts it
// back as it was.
const (
	rolesOrg     = "upgrade-iam"
	rolesOwner   = "owner@upgrade-iam.example.com"
	rolesMember  = "member@upgrade-iam.example.com"
	rolesGuest   = "guest@upgrade-iam.example.com"
	rolesAccount = "shared"
)

// Roles is the roles scenario.
func Roles() Scenario {
	var (
		org     = tenant.OrganizationPath(rolesOrg)
		account = tenant.AccountPath(rolesOrg, rolesAccount)
	)
	identity := []identity{
		accountIdentity(tenant.OrgsPath, rolesOrg),
		organizationIdentity(rolesOrg),
		accountIdentity(org, rolesAccount),
	}

	return Scenario{
		Name: "roles",

		// ORDER MATTERS. Every step builds on the ones before it.
		Seed: []Step{
			// 1. The organization, and its owner able to log in: the
			//    owner is who grants the role in step 5.
			{Name: "organization", Run: seedOrganization(rolesOrg, rolesOwner)},
			{Name: "password of the owner", Run: seedPassword(rolesOrg, rolesOwner)},
			// 2. The two colleagues. An invite makes its user a user of
			//    the realm, so the password comes after the invite.
			{Name: "invite of the member", Run: seedInvite(rolesOrg, "member", rolesMember)},
			{Name: "password of the member", Run: seedPassword(rolesOrg, rolesMember)},
			{Name: "invite of the guest", Run: seedInvite(rolesOrg, "guest", rolesGuest)},
			{Name: "password of the guest", Run: seedPassword(rolesOrg, rolesGuest)},
			// 3. The account the role is on, with data to read in it.
			{Name: "account", Run: seedAccount(org, rolesAccount, rolesOwner)},
			{Name: "data in the account", Run: seedData(account)},
			// 4. After the account and both users: the role.
			{Name: "member role", Run: grantRole(rolesOrg, rolesAccount, rolesOwner, rolesMember, iam.RoleMember)},
			{Name: "identities", Run: seedIdentity(rolesOrg, identity...)},
		},

		Validate: []Step{
			{Name: "organization", Run: validateOrganization(rolesOrg, rolesOwner)},
			{Name: "account", Run: validateAccount(org, rolesAccount)},
			{Name: "data in the account", Run: validateData(account)},
			{Name: "identities", Run: validateIdentity(rolesOrg, identity...)},
			// What the IAM service reports, and then what kcp enforces:
			// the two have to agree, and both come from the tuples.
			{Name: "roles of the account", Run: validateRoles(rolesOrg, rolesAccount, rolesOwner, map[string][]string{
				rolesOwner:  {iam.RoleOwner},
				rolesMember: {iam.RoleMember},
			})},
			{Name: "member reads the account", Run: validateReads(rolesOrg, rolesMember, account)},
			{Name: "member does not write", Run: validateDoesNotWrite(rolesOrg, rolesMember, account)},
			{Name: "guest is kept out", Run: validateDoesNotRead(rolesOrg, rolesGuest, account)},
		},

		// ORDER MATTERS. Each step checks or undoes the one before it.
		After: []Step{
			// 1. Roles change hands, on the store the release filled:
			//    a tuple of the release is deleted, and a new one
			//    written next to the old ones.
			{Name: "take the role from the member", Run: revokeRole(rolesOrg, rolesAccount, rolesOwner, rolesMember, iam.RoleMember)},
			{Name: "member is kept out", Run: eventuallyDoesNotRead(rolesOrg, rolesMember, account)},
			{Name: "give the role to the guest", Run: grantRole(rolesOrg, rolesAccount, rolesOwner, rolesGuest, iam.RoleMember)},
			{Name: "guest reads the account", Run: validateReads(rolesOrg, rolesGuest, account)},
			{Name: "roles of the account", Run: validateRoles(rolesOrg, rolesAccount, rolesOwner, map[string][]string{
				rolesOwner: {iam.RoleOwner},
				rolesGuest: {iam.RoleMember},
			})},
			// 2. And back, so that Validate holds again on a kept
			//    cluster.
			{Name: "take the role from the guest", Run: revokeRole(rolesOrg, rolesAccount, rolesOwner, rolesGuest, iam.RoleMember)},
			{Name: "guest is kept out", Run: eventuallyDoesNotRead(rolesOrg, rolesGuest, account)},
			{Name: "give the role back to the member", Run: grantRole(rolesOrg, rolesAccount, rolesOwner, rolesMember, iam.RoleMember)},
			{Name: "member reads the account", Run: validateReads(rolesOrg, rolesMember, account)},
		},
	}
}

// readData reads the ConfigMap of seedData in the default namespace of the
// workspace at path, as the user, logged in to the organization.
func readData(ctx context.Context, env Environment, org, email, path string) error {
	cl, err := userClient(ctx, env, org, email, path)
	if err != nil {
		return err
	}
	return cl.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: metav1.NamespaceDefault, Name: dataName}, &corev1.ConfigMap{})
}

// validateReads: the user, logged in, reads the data in the workspace at
// path.
func validateReads(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		if err := readData(ctx, env, org, email, path); err != nil {
			return fmt.Errorf("%s reading the data in %s: %w", email, path, err)
		}
		return nil
	}
}

// validateDoesNotRead: the user, logged in, is not allowed to read the data
// in the workspace at path.
func validateDoesNotRead(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		return denied(readData(ctx, env, org, email, path), fmt.Sprintf("%s reading the data in %s", email, path))
	}
}

// eventuallyDoesNotRead is validateDoesNotRead for right after the user
// lost a role: it is called again until the change went through.
func eventuallyDoesNotRead(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		return deniedSoon(readData(ctx, env, org, email, path), fmt.Sprintf("%s reading the data in %s", email, path))
	}
}

// validateDoesNotWrite: the user, logged in, is not allowed to create a
// ConfigMap in the workspace at path. Nothing is created: the request is a
// dry run, which is authorized like any other.
func validateDoesNotWrite(org, email, path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := userClient(ctx, env, org, email, path)
		if err != nil {
			return err
		}
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: dataName + "-denied"}}
		return denied(cl.Create(ctx, cm, ctrlruntimeclient.DryRunAll), fmt.Sprintf("%s creating a configmap in %s", email, path))
	}
}
