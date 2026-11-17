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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/idpactivation"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	epochAnnotation    = "identity.platform-mesh.io/activation-epoch"
	approvalAnnotation = "identity.platform-mesh.io/activation-record-uid"
)

type Publisher struct {
	Organization              *OrganizationReconciler
	Gate                      idpactivation.Gate
	InstallationID            string
	ClientConfigurationDigest string
	OrganizationWorkspacePath string
}

func (p *Publisher) Reconcile(ctx context.Context, workspaceClient ctrlruntimeclient.Client, cluster logicalcluster.Name) (*OrganizationObservation, error) {
	if p.Organization == nil || p.Organization.Classes == nil || p.Gate == nil ||
		p.InstallationID == "" || !idpactivation.Digest(p.ClientConfigurationDigest) ||
		p.OrganizationWorkspacePath == "" {
		return nil, errors.New("identity publication requires trusted orchestration and coordinated activation")
	}
	class := &pmidentityv1alpha1.IdentityProviderClass{}
	if err := p.Organization.Classes.Get(ctx, ctrlruntimeclient.ObjectKey{Name: p.Organization.ClassName}, class); err != nil {
		return nil, err
	}
	binding := idpactivation.Binding{
		InstallationID: p.InstallationID, OrganizationCluster: p.Organization.OrganizationCluster.String(),
		ProviderClassUID: class.UID, ConfigurationRevision: class.Status.ObservedConfigurationRevision,
		ClientConfigurationDigest: p.ClientConfigurationDigest,
	}
	permit, err := p.Gate.Check(ctx, binding)
	if err != nil {
		return nil, err
	}
	if permit.Epoch == "" || permit.RecordUID == "" || permit.ResourceVersion == "" {
		return nil, errors.New("identity activation returned an incomplete permit")
	}
	if err := p.preflightAccountInfo(ctx, workspaceClient, cluster, binding, permit); err != nil {
		return nil, err
	}
	engine := *p.Organization
	engine.ExpectedClassUID = binding.ProviderClassUID
	engine.ExpectedConfigurationRevision = binding.ConfigurationRevision
	checkTenantWrite := func(existing *pmidentityv1alpha1.IdentityTenant, logicalClusterUID types.UID) error {
		if existing != nil && (existing.Annotations[epochAnnotation] != permit.Epoch ||
			existing.Annotations[approvalAnnotation] != string(permit.RecordUID) ||
			existing.Annotations[workspaceUIDAnnotation] != string(logicalClusterUID)) {
			return errors.New("identity tenant requires explicit coordinated migration approval")
		}
		return checkPermit(ctx, p.Gate, binding, permit)
	}
	observation, err := engine.ReconcileActivated(ctx, workspaceClient, cluster, permit, checkTenantWrite)
	if err != nil && (!errors.Is(err, ErrIdentityNotReady) || observation == nil || !observation.TenantCreated) {
		return observation, err
	}
	if observation.TenantCreated {
		if err != nil {
			return observation, err
		}
	}
	info := &pmcorev1alpha1.AccountInfo{}
	if err := workspaceClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "account"}, info); err != nil {
		return observation, err
	}
	if info.UID != observation.AccountInfoUID || !info.DeletionTimestamp.IsZero() ||
		logicalcluster.From(info) != cluster || info.Spec.Account.GeneratedClusterId != cluster.String() ||
		info.Spec.Organization.Name != observation.Tenant.Spec.Organization {
		return observation, errors.New("AccountInfo binding changed while preparing authentication")
	}
	if info.Spec.OIDC != nil {
		identity := info.Spec.OIDC.Identity
		if identity == nil && !managedByActivation(info, permit, observation) {
			return observation, errors.New("legacy AccountInfo requires explicit coordinated migration approval")
		}
		if identity != nil && (identity.Tenant.UID != observation.Tenant.UID ||
			identity.Tenant.LogicalCluster != engine.OrganizationCluster.String() ||
			identity.ProviderClassUID != binding.ProviderClassUID ||
			info.Spec.OIDC.IssuerURL != observation.OIDC.IssuerURL) {
			return observation, errors.New("AccountInfo identity replacement requires coordinated migration")
		}
	}
	if existingEpoch := info.Annotations[epochAnnotation]; existingEpoch != "" && !managedByActivation(info, permit, observation) {
		return observation, errors.New("AccountInfo belongs to a different activation epoch")
	}
	if !observation.TenantCreated && !managedByActivation(observation.Tenant, permit, observation) {
		if observation.Tenant.Annotations[epochAnnotation] != "" {
			return observation, errors.New("identity tenant belongs to a different activation epoch")
		}
		return observation, errors.New("identity tenant requires explicit coordinated migration approval")
	} else if observation.TenantCreated && !managedByActivation(observation.Tenant, permit, observation) {
		return observation, errors.New("new identity tenant activation ownership was not persisted")
	}
	account := &pmcorev1alpha1.Account{}
	if err := engine.Organizations.Get(ctx, ctrlruntimeclient.ObjectKey{Name: observation.Tenant.Spec.Organization}, account); err != nil {
		return observation, err
	}
	workspace := &kcptenancyv1alpha1.Workspace{}
	if err := engine.Organizations.Get(ctx, ctrlruntimeclient.ObjectKey{Name: account.Name}, workspace); err != nil {
		return observation, err
	}
	if account.UID != observation.AccountUID || !account.DeletionTimestamp.IsZero() ||
		workspace.UID != observation.WorkspaceUID || workspace.Spec.Cluster != cluster.String() ||
		!workspace.DeletionTimestamp.IsZero() || !ownedByAccount(workspace, account) ||
		workspace.Spec.Type == nil || !p.localTypePath(workspace.Spec.Type.Path) {
		return observation, errors.New("organization Account/Workspace changed before authentication publication")
	}
	types, err := p.workspaceTypes(ctx, account, workspace, permit, observation)
	if err != nil {
		return observation, err
	}
	authName := authenticationConfigurationName(account.Name, permit.Epoch)
	auth := &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{}
	err = engine.Organizations.Get(ctx, ctrlruntimeclient.ObjectKey{Name: authName}, auth)
	create := apierrors.IsNotFound(err)
	if err != nil && !create {
		return observation, err
	}
	if create {
		auth = &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: authName, OwnerReferences: []metav1.OwnerReference{{
					APIVersion: pmcorev1alpha1.SchemeGroupVersion.String(), Kind: "Account", Name: account.Name, UID: account.UID,
				}},
			},
		}
	} else if !ownedByAccount(auth, account) || !managedByActivation(auth, permit, observation) ||
		logicalcluster.From(auth) != engine.OrganizationCluster || !auth.DeletionTimestamp.IsZero() {
		return observation, errors.New("authentication configuration requires explicit ownership/epoch migration")
	}
	desiredAuth := &kcptenancyv1alpha1.WorkspaceAuthenticationConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: authName, OwnerReferences: []metav1.OwnerReference{{
				APIVersion: pmcorev1alpha1.SchemeGroupVersion.String(), Kind: "Account", Name: account.Name, UID: account.UID,
			}},
		},
		Spec: kcptenancyv1alpha1.WorkspaceAuthenticationConfigurationSpec{
			JWT: []kcptenancyv1alpha1.JWTAuthenticator{*observation.Authenticator},
		},
	}
	stampActivation(desiredAuth, permit, observation)
	if !create && !equality.Semantic.DeepEqual(auth.Spec, desiredAuth.Spec) {
		return observation, errors.New("activation-scoped workspace authentication is immutable; issue a new activation epoch")
	}
	check := func() error { return checkPermit(ctx, p.Gate, binding, permit) }
	if create {
		if err := check(); err != nil {
			return observation, err
		}
		err = engine.Organizations.Create(ctx, desiredAuth)
		if err != nil {
			return observation, err
		}
	}
	beforeInfo := info.DeepCopy()
	info.Spec.OIDC = observation.OIDC
	stampActivation(info, permit, observation)
	if !equality.Semantic.DeepEqual(beforeInfo, info) {
		if err := check(); err != nil {
			return observation, err
		}
		if err := workspaceClient.Patch(ctx, info, ctrlruntimeclient.MergeFromWithOptions(beforeInfo, ctrlruntimeclient.MergeFromWithOptimisticLock{})); err != nil {
			return observation, err
		}
	}
	for _, workspaceType := range types {
		before := workspaceType.DeepCopy()
		workspaceType.Spec.AuthenticationConfigurations = []kcptenancyv1alpha1.AuthenticationConfigurationReference{{Name: authName}}
		stampActivation(workspaceType, permit, observation)
		if equality.Semantic.DeepEqual(before, workspaceType) {
			continue
		}
		if err := check(); err != nil {
			return observation, err
		}
		if err := engine.Organizations.Patch(ctx, workspaceType, ctrlruntimeclient.MergeFromWithOptions(before, ctrlruntimeclient.MergeFromWithOptimisticLock{})); err != nil {
			return observation, err
		}
	}
	return observation, nil
}

func (p *Publisher) localTypePath(path string) bool {
	return path == p.OrganizationWorkspacePath || path == p.Organization.OrganizationCluster.String()
}

func (p *Publisher) preflightAccountInfo(
	ctx context.Context,
	workspaceClient ctrlruntimeclient.Client,
	cluster logicalcluster.Name,
	binding idpactivation.Binding,
	permit idpactivation.Permit,
) error {
	info := &pmcorev1alpha1.AccountInfo{}
	if err := workspaceClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "account"}, info); err != nil {
		return err
	}
	logicalCluster := &kcpcorev1alpha1.LogicalCluster{}
	if err := workspaceClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "cluster"}, logicalCluster); err != nil {
		return err
	}
	if info.UID == "" || !info.DeletionTimestamp.IsZero() || logicalcluster.From(info) != cluster ||
		logicalcluster.From(logicalCluster) != cluster || logicalCluster.UID == "" ||
		info.Spec.Account.GeneratedClusterId != cluster.String() ||
		info.Spec.Account.Type != pmcorev1alpha1.AccountTypeOrg ||
		info.Spec.Organization.Type != pmcorev1alpha1.AccountTypeOrg ||
		info.Spec.Organization.Name == "" {
		return errors.New("AccountInfo or LogicalCluster is not bound to this organization workspace")
	}
	if epoch := info.Annotations[epochAnnotation]; epoch != "" &&
		!activationOwnership(info, permit, logicalCluster.UID) {
		return errors.New("AccountInfo belongs to a different activation epoch")
	}
	if info.Spec.OIDC == nil {
		return nil
	}
	identity := info.Spec.OIDC.Identity
	if identity == nil {
		return errors.New("legacy AccountInfo requires explicit coordinated migration approval")
	}
	if info.Spec.OIDC.IssuerURL == "" || identity.Tenant.Name != info.Spec.Organization.Name ||
		identity.Tenant.LogicalCluster != p.Organization.OrganizationCluster.String() ||
		identity.ProviderClassRef != p.Organization.ClassName ||
		identity.ProviderClassUID != binding.ProviderClassUID ||
		!activationOwnership(info, permit, logicalCluster.UID) {
		return errors.New("existing AccountInfo identity requires explicit coordinated migration")
	}
	tenant := &pmidentityv1alpha1.IdentityTenant{}
	if err := p.Organization.Organizations.Get(ctx, ctrlruntimeclient.ObjectKey{Name: identity.Tenant.Name}, tenant); err != nil {
		return fmt.Errorf("resolve existing AccountInfo identity tenant: %w", err)
	}
	if tenant.UID != identity.Tenant.UID || tenant.Spec.Organization != info.Spec.Organization.Name ||
		tenant.Spec.ProviderClassRef != identity.ProviderClassRef ||
		tenant.Status.IssuerURL != info.Spec.OIDC.IssuerURL ||
		!managedByActivation(tenant, permit, &OrganizationObservation{LogicalClusterUID: logicalCluster.UID}) {
		return errors.New("existing AccountInfo identity tenant binding requires coordinated migration")
	}
	return nil
}

func (p *Publisher) workspaceTypes(ctx context.Context, account *pmcorev1alpha1.Account, workspace *kcptenancyv1alpha1.Workspace, permit idpactivation.Permit, observation *OrganizationObservation) ([]*kcptenancyv1alpha1.WorkspaceType, error) {
	orgType := &kcptenancyv1alpha1.WorkspaceType{}
	if err := p.Organization.Organizations.Get(ctx, ctrlruntimeclient.ObjectKey{Name: string(workspace.Spec.Type.Name)}, orgType); err != nil {
		return nil, err
	}
	child := orgType.Spec.DefaultChildWorkspaceType
	if child == nil || string(child.Name) == orgType.Name || !p.localTypePath(child.Path) {
		return nil, errors.New("organization type requires a distinct local account type")
	}
	accountType := &kcptenancyv1alpha1.WorkspaceType{}
	if err := p.Organization.Organizations.Get(ctx, ctrlruntimeclient.ObjectKey{Name: string(child.Name)}, accountType); err != nil {
		return nil, err
	}
	types := []*kcptenancyv1alpha1.WorkspaceType{orgType, accountType}
	for _, workspaceType := range types {
		if logicalcluster.From(workspaceType) != p.Organization.OrganizationCluster || workspaceType.UID == "" ||
			!workspaceType.DeletionTimestamp.IsZero() || workspaceType.Labels["core.platform-mesh.io/org"] != account.Name ||
			(len(workspaceType.OwnerReferences) != 0 && !ownedByAccount(workspaceType, account)) {
			return nil, errors.New("workspace type is not bound to this organization")
		}
		if (len(workspaceType.Spec.AuthenticationConfigurations) != 0 || workspaceType.Annotations[epochAnnotation] != "") &&
			!managedByActivation(workspaceType, permit, observation) {
			return nil, errors.New("existing workspace-type authentication requires coordinated migration")
		}
		if managedByActivation(workspaceType, permit, observation) &&
			!slices.EqualFunc(workspaceType.Spec.AuthenticationConfigurations,
				[]kcptenancyv1alpha1.AuthenticationConfigurationReference{{Name: authenticationConfigurationName(account.Name, permit.Epoch)}},
				func(a, b kcptenancyv1alpha1.AuthenticationConfigurationReference) bool { return a == b }) {
			return nil, fmt.Errorf("managed workspace type %s authentication was changed by another writer", workspaceType.Name)
		}
	}
	return types, nil
}

func authenticationConfigurationName(organization, epoch string) string {
	digest := sha256.Sum256([]byte(epoch))
	suffix := hex.EncodeToString(digest[:6])
	const maxNameLength = 253
	if len(organization) > maxNameLength-len("identity-")-len(suffix) {
		organization = organization[:maxNameLength-len("identity-")-len(suffix)]
	}
	return "identity-" + organization + "-" + suffix
}

func checkPermit(ctx context.Context, gate idpactivation.Gate, binding idpactivation.Binding, expected idpactivation.Permit) error {
	current, err := gate.Check(ctx, binding)
	if err != nil {
		return err
	}
	if current != expected {
		return errors.New("identity activation changed during publication")
	}
	return nil
}

func managedByActivation(object ctrlruntimeclient.Object, permit idpactivation.Permit, observation *OrganizationObservation) bool {
	return activationOwnership(object, permit, observation.LogicalClusterUID)
}

func activationOwnership(object ctrlruntimeclient.Object, permit idpactivation.Permit, workspaceUID types.UID) bool {
	annotations := object.GetAnnotations()
	return annotations[epochAnnotation] == permit.Epoch &&
		annotations[approvalAnnotation] == string(permit.RecordUID) &&
		annotations[workspaceUIDAnnotation] == string(workspaceUID)
}

func stampActivation(object ctrlruntimeclient.Object, permit idpactivation.Permit, observation *OrganizationObservation) {
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[epochAnnotation], annotations[approvalAnnotation] = permit.Epoch, string(permit.RecordUID)
	annotations[workspaceUIDAnnotation] = string(observation.LogicalClusterUID)
	object.SetAnnotations(annotations)
}
