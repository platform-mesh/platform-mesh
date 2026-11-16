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

package orchestration

import (
	"context"
	"errors"
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/apis/identity/authentication"
	"go.platform-mesh.io/apis/identity/projection"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/idpactivation"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const workspaceUIDAnnotation = "identity.platform-mesh.io/organization-workspace-uid"

var ErrIdentityNotReady = errors.New("organization identity is not ready")

// OrganizationReconciler owns identity intent, not the provider implementation.
// Authentication publication is deliberately left to coordinated activation.
type OrganizationReconciler struct {
	Organizations                 ctrlruntimeclient.Client
	Classes                       ctrlruntimeclient.Reader
	OrganizationCluster           logicalcluster.Name
	ClassName                     string
	ExpectedClassUID              types.UID
	ExpectedConfigurationRevision string
	Clients                       func(string) ([]pmidentityv1alpha1.OIDCClient, error)
	RegistrationAllowed           *bool
	CertificateAuthority          string
}

type OrganizationObservation struct {
	AccountUID, AccountInfoUID, WorkspaceUID, LogicalClusterUID types.UID
	TenantCreated                                               bool
	Tenant                                                      *pmidentityv1alpha1.IdentityTenant
	OIDC                                                        *pmcorev1alpha1.OIDCInfo
	Authenticator                                               *kcptenancyv1alpha1.JWTAuthenticator
}

func (r *OrganizationReconciler) Reconcile(ctx context.Context, workspaceClient ctrlruntimeclient.Client, cluster logicalcluster.Name) (*OrganizationObservation, error) {
	return r.reconcile(ctx, workspaceClient, cluster, nil, nil)
}

func (r *OrganizationReconciler) ReconcileActivated(
	ctx context.Context,
	workspaceClient ctrlruntimeclient.Client,
	cluster logicalcluster.Name,
	permit idpactivation.Permit,
	beforeWrite func(*pmidentityv1alpha1.IdentityTenant, types.UID) error,
) (*OrganizationObservation, error) {
	if permit.Epoch == "" || permit.RecordUID == "" || permit.ResourceVersion == "" || beforeWrite == nil {
		return nil, errors.New("activation permit and pre-write authorization check are required")
	}
	return r.reconcile(ctx, workspaceClient, cluster, &permit, beforeWrite)
}

func (r *OrganizationReconciler) reconcile(
	ctx context.Context,
	workspaceClient ctrlruntimeclient.Client,
	cluster logicalcluster.Name,
	permit *idpactivation.Permit,
	beforeWrite func(*pmidentityv1alpha1.IdentityTenant, types.UID) error,
) (*OrganizationObservation, error) {
	if r.Organizations == nil || r.Classes == nil || workspaceClient == nil || !cluster.IsValid() ||
		!r.OrganizationCluster.IsValid() || r.ClassName == "" || r.Clients == nil {
		return nil, errors.New("trusted organization/class clients, immutable clusters and explicit client configuration are required")
	}
	info := &pmcorev1alpha1.AccountInfo{}
	if err := workspaceClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "account"}, info); err != nil {
		return nil, err
	}
	accountLocation, organization := info.Spec.Account, info.Spec.Organization
	if info.UID == "" || logicalcluster.From(info) != cluster || accountLocation.Type != pmcorev1alpha1.AccountTypeOrg ||
		organization.Type != pmcorev1alpha1.AccountTypeOrg || accountLocation.Name == "" ||
		accountLocation.Name != organization.Name || accountLocation.GeneratedClusterId != cluster.String() ||
		organization.GeneratedClusterId != cluster.String() ||
		accountLocation.OriginClusterId != r.OrganizationCluster.String() ||
		organization.OriginClusterId != r.OrganizationCluster.String() {
		return nil, errors.New("workspace AccountInfo does not identify this organization and its trusted origin")
	}
	lc := &kcpcorev1alpha1.LogicalCluster{}
	if err := workspaceClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "cluster"}, lc); err != nil {
		return nil, err
	}
	if logicalcluster.From(lc) != cluster || lc.UID == "" || !lc.DeletionTimestamp.IsZero() {
		return nil, errors.New("organization logical-cluster identity is missing or terminating")
	}
	account := &pmcorev1alpha1.Account{}
	if err := r.Organizations.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountLocation.Name}, account); err != nil {
		return nil, err
	}
	if logicalcluster.From(account) != r.OrganizationCluster || account.UID == "" ||
		account.Spec.Type != pmcorev1alpha1.AccountTypeOrg || !account.DeletionTimestamp.IsZero() {
		return nil, errors.New("trusted organization Account is invalid or terminating")
	}
	workspace := &kcptenancyv1alpha1.Workspace{}
	if err := r.Organizations.Get(ctx, ctrlruntimeclient.ObjectKey{Name: account.Name}, workspace); err != nil {
		return nil, err
	}
	if logicalcluster.From(workspace) != r.OrganizationCluster || workspace.UID == "" ||
		workspace.Spec.Cluster != cluster.String() || !workspace.DeletionTimestamp.IsZero() ||
		!ownedByAccount(workspace, account) {
		return nil, errors.New("organization Workspace does not bind this cluster to the exact Account UID")
	}
	class := &pmidentityv1alpha1.IdentityProviderClass{}
	if err := r.Classes.Get(ctx, ctrlruntimeclient.ObjectKey{Name: r.ClassName}, class); err != nil {
		return nil, err
	}
	if errs := pmidentityv1alpha1.ValidateProviderClass(class); len(errs) != 0 {
		return nil, errs.ToAggregate()
	}
	if class.UID == "" || !class.DeletionTimestamp.IsZero() {
		return nil, errors.New("selected provider class has no live immutable identity")
	}
	if r.ExpectedClassUID != "" && class.UID != r.ExpectedClassUID {
		return nil, errors.New("provider class changed after coordinated activation was checked")
	}
	if r.ExpectedConfigurationRevision != "" &&
		class.Status.ObservedConfigurationRevision != r.ExpectedConfigurationRevision {
		return nil, errors.New("provider configuration changed after coordinated activation was checked")
	}
	clients, err := r.Clients(account.Name)
	if err != nil {
		return nil, fmt.Errorf("resolve administrator-configured organization clients: %w", err)
	}
	if len(clients) == 0 {
		return nil, errors.New("organization identity requires explicitly configured OIDC clients")
	}
	desired := (&pmidentityv1alpha1.IdentityTenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: account.Name, Annotations: map[string]string{workspaceUIDAnnotation: string(lc.UID)},
		},
		Spec: pmidentityv1alpha1.IdentityTenantSpec{
			ProviderClassRef: class.Name, Purpose: pmidentityv1alpha1.IdentityTenantPurposeOrganization,
			Organization: account.Name, RegistrationAllowed: r.RegistrationAllowed,
			Clients: clients, DeletionPolicy: pmidentityv1alpha1.DeletionPolicyDelete,
		},
	}).DeepCopy()
	if permit != nil {
		stampActivation(desired, *permit, &OrganizationObservation{LogicalClusterUID: lc.UID})
	}
	if errs := pmidentityv1alpha1.ValidateTenant(desired); len(errs) != 0 {
		return nil, errs.ToAggregate()
	}
	tenant := &pmidentityv1alpha1.IdentityTenant{}
	tenantCreated := false
	err = r.Organizations.Get(ctx, ctrlruntimeclient.ObjectKey{Name: desired.Name}, tenant)
	switch {
	case apierrors.IsNotFound(err):
		if err := controllerutil.SetOwnerReference(account, desired, r.Organizations.Scheme()); err != nil {
			return nil, err
		}
		if beforeWrite != nil {
			if err := beforeWrite(nil, lc.UID); err != nil {
				return nil, err
			}
		}
		if err := r.Organizations.Create(ctx, desired); err != nil {
			return nil, err
		}
		tenant = desired
		tenantCreated = true
	case err != nil:
		return nil, err
	default:
		if logicalcluster.From(tenant) != r.OrganizationCluster || !tenant.DeletionTimestamp.IsZero() ||
			!ownedByAccount(tenant, account) || tenant.Annotations[workspaceUIDAnnotation] != string(lc.UID) {
			return nil, errors.New("identity tenant is not owned by this Account and organization workspace; implicit adoption is forbidden")
		}
		if tenant.Spec.ProviderClassRef != class.Name || tenant.Spec.Purpose != desired.Spec.Purpose ||
			tenant.Spec.Organization != account.Name || tenant.Spec.AdoptionRef != "" ||
			tenant.Spec.DeletionPolicy != pmidentityv1alpha1.DeletionPolicyDelete ||
			(tenant.Status.BoundProviderClassUID != "" && tenant.Status.BoundProviderClassUID != class.UID) {
			return nil, errors.New("identity tenant binding or lifecycle policy requires coordinated migration")
		}
		if beforeWrite != nil {
			if err := beforeWrite(tenant, lc.UID); err != nil {
				return nil, err
			}
		}
		if !equality.Semantic.DeepEqual(tenant.Spec, desired.Spec) {
			tenant.Spec = desired.Spec
			if err := r.Organizations.Update(ctx, tenant); err != nil {
				return nil, err
			}
		}
	}
	result := &OrganizationObservation{
		AccountUID: account.UID, AccountInfoUID: info.UID, WorkspaceUID: workspace.UID,
		LogicalClusterUID: lc.UID, TenantCreated: tenantCreated, Tenant: tenant,
	}
	authenticator, err := authentication.Resolve(class, tenant, r.CertificateAuthority)
	if err != nil {
		return result, fmt.Errorf("%w: %w", ErrIdentityNotReady, err)
	}
	oidc, err := projection.AccountInfo(class, tenant)
	if err != nil {
		return result, err
	}
	result.OIDC, result.Authenticator = oidc, &authenticator
	return result, nil
}

func ownedByAccount(object ctrlruntimeclient.Object, account *pmcorev1alpha1.Account) bool {
	references := object.GetOwnerReferences()
	if len(references) != 1 {
		return false
	}
	owner := references[0]
	version, err := schema.ParseGroupVersion(owner.APIVersion)
	return err == nil && version.Group == pmcorev1alpha1.GroupName &&
		owner.Kind == "Account" && owner.Name == account.Name && owner.UID == account.UID
}
