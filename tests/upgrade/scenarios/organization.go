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

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/keycloak"
	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// The steps of the tenant scenario (tenant.go) about the
// organization: the organization itself, its owner, and who is allowed in.

// seedOrganization creates the organization for its owner.
func seedOrganization(ctx context.Context, env Environment) error {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return err
	}
	_, err = tenant.ApplyOrganization(ctx, kcp, tenantOrg, tenantOwner)
	return err
}

// seedOwnerPassword gives the owner a password. Platform Mesh invites the
// owner while the organization gets Ready, and an invited user would have to
// set a password on the first login.
func seedOwnerPassword(ctx context.Context, env Environment) error {
	kc, err := env.Keycloak(ctx)
	if err != nil {
		return err
	}
	_, err = kc.EnsureUser(ctx, tenantOrg, keycloak.User{Email: tenantOwner, FirstName: "Owner", LastName: "Seed", Password: tenantPassword})
	return err
}

// validateOrganization: the organization is Ready, belongs to its owner and
// has its workspace.
func validateOrganization(ctx context.Context, env Environment) error {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return err
	}
	org, err := tenant.GetOrganization(ctx, kcp, tenantOrg)
	if err != nil {
		return err
	}
	if org.Spec.Creator == nil || *org.Spec.Creator != tenantOwner {
		return steps.Terminal("organization %s has creator %v, not %s", tenantOrg, org.Spec.Creator, tenantOwner)
	}
	return workspaceReady(ctx, kcp, tenant.OrgsPath, tenantOrg)
}

// validateOwnerUser: the owner is a user of the realm of the organization
// and can log in with the password of seedOwnerPassword, without further
// steps.
func validateOwnerUser(ctx context.Context, env Environment) error {
	kc, err := env.Keycloak(ctx)
	if err != nil {
		return err
	}
	owner, err := kc.User(ctx, tenantOrg, tenantOwner)
	if err != nil {
		return err
	}
	if !owner.Enabled || len(owner.RequiredActions) != 0 {
		return steps.Terminal("the owner cannot log in without further steps: enabled=%t, required actions %v", owner.Enabled, owner.RequiredActions)
	}
	return nil
}

// validateOwnerAllowed: the owner is allowed into the organization, by the
// authorization tuples written when it was created, and sees its one
// account.
func validateOwnerAllowed(ctx context.Context, env Environment) error {
	cl, err := tenantClient(ctx, env, tenant.OrganizationPath(tenantOrg), tenantOwner)
	if err != nil {
		return err
	}
	accounts := &pmcorev1alpha1.AccountList{}
	if err := cl.List(ctx, accounts); err != nil {
		return fmt.Errorf("listing the accounts of the organization as its owner: %w", err)
	}
	if len(accounts.Items) != 1 || accounts.Items[0].Name != tenantAccount {
		return steps.Terminal("the owner sees %d accounts, not just %s", len(accounts.Items), tenantAccount)
	}
	return nil
}

// validateStrangerDenied: a user with no role in the organization is not
// allowed to list its accounts.
func validateStrangerDenied(ctx context.Context, env Environment) error {
	cl, err := tenantClient(ctx, env, tenant.OrganizationPath(tenantOrg), tenantStranger)
	if err != nil {
		return err
	}
	err = cl.List(ctx, &pmcorev1alpha1.AccountList{})
	switch {
	case err == nil:
		return steps.Terminal("a stranger is allowed to list the accounts of the organization")
	case apierrors.IsForbidden(err):
		return nil
	default:
		return err
	}
}
