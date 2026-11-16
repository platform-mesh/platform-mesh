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

package identityprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/apis/identity/authentication"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/identity"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/directory"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kcp-dev/logicalcluster/v3"
)

const (
	invitationFinalizer = "identity.platform-mesh.io/keycloak-invitation"
	invitationJournal   = "invitation.json"
)

type invitationInventory struct {
	Version          int         `json:"version"`
	Owner            string      `json:"owner"`
	ProviderIdentity string      `json:"providerIdentity"`
	ClassUID         types.UID   `json:"classUID"`
	TenantUID        types.UID   `json:"tenantUID"`
	Realm            string      `json:"realm"`
	Issuer           string      `json:"issuer"`
	ClientID         string      `json:"clientID"`
	Redirect         string      `json:"redirect"`
	Email            string      `json:"email"`
	Subject          string      `json:"subject,omitempty"`
	ExpiresAt        metav1.Time `json:"expiresAt"`
	Dispatched       bool        `json:"dispatched"`
}

// InvitationReconciler owns request delivery only. It never writes role grants,
// deletes provider users, or interprets a provisioned user as verified acceptance.
type InvitationReconciler struct {
	Requests, Tenants, Private ctrlruntimeclient.Client
	Cluster, TenantCluster     logicalcluster.Name
	RuntimeNamespace           string
	ProviderIdentity           string
	Backend                    InvitationBackend
	Now                        func() time.Time
}

func (r *InvitationReconciler) Reconcile(ctx context.Context, class *pmidentityv1alpha1.IdentityProviderClass, invitation *pmidentityv1alpha1.Invitation) error {
	if r.Requests == nil || r.Private == nil || r.RuntimeNamespace == "" || !r.Cluster.IsValid() ||
		invitation == nil || invitation.UID == "" || logicalcluster.From(invitation) != r.Cluster {
		return errors.New("invitation request client, private namespace and immutable KCP identity are required")
	}
	owner := "invitation/" + r.Cluster.String() + "/" + string(invitation.UID)
	if !invitation.DeletionTimestamp.IsZero() {
		return r.finalizeInvitation(ctx, invitation, owner)
	}
	if errs := pmidentityv1alpha1.ValidateInvitation(invitation); len(errs) != 0 {
		return errs.ToAggregate()
	}
	if r.Tenants == nil || r.Backend == nil || r.ProviderIdentity == "" || !r.TenantCluster.IsValid() ||
		invitation.CreationTimestamp.IsZero() || class == nil || class.UID == "" ||
		class.Spec.ControllerName != directory.KeycloakControllerName ||
		class.Spec.AuthenticationPolicy.IssuerMode != pmidentityv1alpha1.IssuerModeDedicated ||
		!slices.Contains(class.Status.Capabilities, pmidentityv1alpha1.IdentityCapabilityInvitations) {
		return errors.New("current invitation provider, creation timestamp and trusted tenant context are required")
	}
	accountInfo := &pmcorev1alpha1.AccountInfo{}
	if err := r.Requests.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "account"}, accountInfo); err != nil {
		return err
	}
	context := accountInfo.Spec.OIDC
	if context == nil || context.Identity == nil || logicalcluster.From(accountInfo) != r.Cluster {
		return errors.New("invitation workspace has no UID-bound identity projection")
	}
	binding := context.Identity
	if binding.Tenant.LogicalCluster != r.TenantCluster.String() || binding.Tenant.Name != invitation.Spec.TenantRef ||
		binding.ProviderClassRef != class.Name || binding.ProviderClassUID != class.UID {
		return errors.New("invitation cannot select a different tenant, provider or workspace")
	}
	tenant := &pmidentityv1alpha1.IdentityTenant{}
	if err := r.Tenants.Get(ctx, ctrlruntimeclient.ObjectKey{Name: binding.Tenant.Name}, tenant); err != nil {
		return err
	}
	if tenant.UID != binding.Tenant.UID || logicalcluster.From(tenant) != r.TenantCluster ||
		tenant.Spec.Purpose != pmidentityv1alpha1.IdentityTenantPurposeOrganization ||
		accountInfo.Spec.Organization.Type != pmcorev1alpha1.AccountTypeOrg ||
		accountInfo.Spec.Account.GeneratedClusterId != r.Cluster.String() ||
		accountInfo.Spec.Organization.OriginClusterId != r.TenantCluster.String() ||
		accountInfo.Spec.Organization.Name != tenant.Spec.Organization ||
		context.IssuerURL != tenant.Status.IssuerURL ||
		binding.ConfigurationRevision != tenant.Status.ObservedConfigurationRevision {
		return errors.New("invitation identity projection is stale or does not belong to this organization")
	}
	if _, err := authentication.Resolve(class, tenant, ""); err != nil {
		return err
	}
	var selected *pmidentityv1alpha1.OIDCClient
	for i := range tenant.Spec.Clients {
		if tenant.Spec.Clients[i].Name == invitation.Spec.ClientName {
			selected = &tenant.Spec.Clients[i]
			break
		}
	}
	if selected == nil || !slices.Contains(selected.GrantTypes, "authorization_code") || len(selected.RedirectURIs) == 0 {
		return errors.New("invitation requires a configured interactive client and exact callback")
	}
	clientID := ""
	for _, client := range tenant.Status.Clients {
		if client.Name == selected.Name {
			clientID = client.ClientID
		}
	}
	if !controllerutil.ContainsFinalizer(invitation, invitationFinalizer) {
		controllerutil.AddFinalizer(invitation, invitationFinalizer)
		if err := r.Requests.Update(ctx, invitation); err != nil {
			return err
		}
	}
	secret := &corev1.Secret{}
	err := r.Private.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: r.RuntimeNamespace, Name: privateName(owner)}, secret)
	state := &invitationInventory{}
	if apierrors.IsNotFound(err) {
		if invitation.Status.ExternalID != "" || invitation.Status.Principal != nil {
			return errors.New("private invitation inventory is missing; refusing to repeat delivery")
		}
		expiry := metav1.NewTime(invitation.CreationTimestamp.Add(24 * time.Hour))
		if invitation.Spec.ExpiresAt != nil {
			expiry = *invitation.Spec.ExpiresAt
		}
		state = &invitationInventory{
			Version: 1, Owner: owner, ProviderIdentity: r.ProviderIdentity, ClassUID: class.UID,
			TenantUID: tenant.UID, Realm: tenant.Status.ExternalID, Issuer: tenant.Status.IssuerURL,
			ClientID: clientID, Redirect: selected.RedirectURIs[0], Email: invitation.Spec.Email, ExpiresAt: expiry,
		}
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: privateName(owner), Namespace: r.RuntimeNamespace, Annotations: map[string]string{ownerAnnotation: owner}},
			Type:       corev1.SecretTypeOpaque, Data: map[string][]byte{invitationJournal: data},
		}
		if err := r.Private.Create(ctx, secret); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if err := json.Unmarshal(secret.Data[invitationJournal], state); err != nil {
		return errors.New("private invitation inventory is invalid")
	}
	if state.Version != 1 || state.Owner != owner || secret.Annotations[ownerAnnotation] != owner ||
		state.ProviderIdentity != r.ProviderIdentity || state.ClassUID != class.UID || state.TenantUID != tenant.UID ||
		state.Realm != tenant.Status.ExternalID || state.Issuer != tenant.Status.IssuerURL ||
		state.ClientID != clientID || state.Email != invitation.Spec.Email || state.ExpiresAt.IsZero() ||
		(state.Dispatched && state.Subject == "") {
		return errors.New("private invitation binding changed; explicit recovery is required")
	}
	now := r.now()
	if !now.Before(state.ExpiresAt.Time) {
		return r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseExpired, nil)
	}
	if state.Dispatched {
		return r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseDispatched, nil)
	}
	if state.Redirect != selected.RedirectURIs[0] {
		return errors.New("invitation callback changed before delivery; a new request is required")
	}
	if math.Ceil(state.ExpiresAt.Sub(now).Seconds()) > math.MaxInt32 {
		return r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseFailed, errors.New("invitation expiration exceeds the provider's supported lifespan"))
	}
	subject, err := r.Backend.EnsureInvitationUser(ctx, state.Realm, r.TenantCluster.String()+"/"+string(state.TenantUID), state.Email, state.Subject)
	if err != nil {
		return r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseFailed, err)
	}
	if state.Subject != "" && subject != state.Subject {
		return r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseFailed, errors.New("invitation principal changed"))
	}
	if err := (identity.Principal{Issuer: state.Issuer, Subject: subject}).Validate(); err != nil {
		return r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseFailed, err)
	}
	state.Subject = subject
	if err := r.saveInvitation(ctx, secret, state); err != nil {
		return err
	}
	if err := r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseProvisioned, nil); err != nil {
		return err
	}
	remaining := state.ExpiresAt.Sub(r.now())
	if remaining <= 0 {
		return r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseExpired, nil)
	}
	lifespan := math.Ceil(remaining.Seconds())
	if lifespan > math.MaxInt32 {
		return r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseFailed, errors.New("invitation expiration exceeds the provider's supported lifespan"))
	}
	err = r.Backend.DispatchInvitation(ctx, state.Realm, r.TenantCluster.String()+"/"+string(state.TenantUID), state.Subject, state.ClientID, state.Redirect, int(lifespan))
	if err != nil {
		return r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseFailed, err)
	}
	state.Dispatched = true
	if err := r.saveInvitation(ctx, secret, state); err != nil {
		return err
	}
	return r.invitationStatus(ctx, invitation, state, pmidentityv1alpha1.InvitationPhaseDispatched, nil)
}

func (r *InvitationReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *InvitationReconciler) saveInvitation(ctx context.Context, secret *corev1.Secret, state *invitationInventory) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	secret.Data[invitationJournal] = data
	return r.Private.Update(ctx, secret)
}

func (r *InvitationReconciler) invitationStatus(ctx context.Context, invitation *pmidentityv1alpha1.Invitation, state *invitationInventory, phase pmidentityv1alpha1.InvitationPhase, failure error) error {
	before := invitation.DeepCopy().Status
	invitation.Status.ObservedGeneration = invitation.Generation
	invitation.Status.ExternalID = "keycloak-invitation/" + string(invitation.UID)
	invitation.Status.Phase = phase
	if state.Subject != "" {
		invitation.Status.Principal = &pmidentityv1alpha1.PrincipalReference{Issuer: state.Issuer, Subject: state.Subject}
	}
	setReady(&invitation.Status.Conditions, invitation.Generation, phase == pmidentityv1alpha1.InvitationPhaseDispatched, string(phase), "Invitation delivery state; acceptance requires verified identity proof")
	condition := metav1.ConditionFalse
	if state.Subject != "" {
		condition = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&invitation.Status.Conditions, metav1.Condition{
		Type: "UserProvisioned", Status: condition, ObservedGeneration: invitation.Generation,
		Reason: "DeliveryTarget", Message: "Provisioning a user does not establish invitation acceptance",
	})
	meta.SetStatusCondition(&invitation.Status.Conditions, metav1.Condition{
		Type: "IdentityVerified", Status: metav1.ConditionFalse, ObservedGeneration: invitation.Generation,
		Reason: "ProofRequired", Message: "Action-email delivery is not an authenticated acceptance proof",
	})
	if equality.Semantic.DeepEqual(before, invitation.Status) {
		return failure
	}
	return errors.Join(failure, r.Requests.Status().Update(ctx, invitation))
}

func (r *InvitationReconciler) finalizeInvitation(ctx context.Context, invitation *pmidentityv1alpha1.Invitation, owner string) error {
	if !controllerutil.ContainsFinalizer(invitation, invitationFinalizer) {
		return nil
	}
	secret := &corev1.Secret{}
	err := r.Private.Get(ctx, ctrlruntimeclient.ObjectKey{Name: privateName(owner), Namespace: r.RuntimeNamespace}, secret)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil {
		if secret.Annotations[ownerAnnotation] != owner {
			return fmt.Errorf("private invitation inventory is not owned by this request")
		}
		if err := r.Private.Delete(ctx, secret, ctrlruntimeclient.Preconditions{UID: &secret.UID, ResourceVersion: &secret.ResourceVersion}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	controllerutil.RemoveFinalizer(invitation, invitationFinalizer)
	return r.Requests.Update(ctx, invitation)
}
