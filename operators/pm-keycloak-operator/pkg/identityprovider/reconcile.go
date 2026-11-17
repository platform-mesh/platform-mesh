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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/clientreg"
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
	Finalizer       = "identity.platform-mesh.io/keycloak"
	ownerAnnotation = "identity.platform-mesh.io/owner"
	journalKey      = "journal.json"
)

type clientState struct {
	Phase              string                      `json:"phase"`
	Info               clientreg.ClientInformation `json:"information"`
	ExternalID         string                      `json:"externalId,omitempty"`
	AppliedHash        string                      `json:"appliedHash,omitempty"`
	Outputs            []string                    `json:"outputs,omitempty"`
	ObservedGeneration int64                       `json:"observedGeneration,omitempty"`
}

type inventory struct {
	Version          int                     `json:"version"`
	Owner            string                  `json:"owner"`
	ProviderIdentity string                  `json:"providerIdentity"`
	ClassUID         types.UID               `json:"classUid"`
	Realm            string                  `json:"realm"`
	Retained         bool                    `json:"retained,omitempty"`
	TenantObserved   bool                    `json:"tenantObserved,omitempty"`
	Clients          map[string]*clientState `json:"clients"`
}

// Reconciler is instantiated with clients for exactly one trusted KCP workspace
// and the private provider namespace. It never writes a tenant-selected workspace.
type Reconciler struct {
	Tenants                           ctrlruntimeclient.Client
	Private                           ctrlruntimeclient.Client
	Cluster                           logicalcluster.Name
	RuntimeNamespace, OutputNamespace string
	ProviderIdentity                  string
	Backend                           Backend
}

func (r *Reconciler) Reconcile(ctx context.Context, class *pmidentityv1alpha1.IdentityProviderClass, tenant *pmidentityv1alpha1.IdentityTenant) error {
	if r.Tenants == nil || r.Private == nil || r.Backend == nil || r.RuntimeNamespace == "" ||
		r.OutputNamespace == "" || r.ProviderIdentity == "" || !r.Cluster.IsValid() {
		return errors.New("identity provider clients, namespaces, provider identity and logical cluster are required")
	}
	if tenant == nil || tenant.UID == "" || logicalcluster.From(tenant) != r.Cluster {
		return errors.New("identity tenant immutable KCP identity is invalid")
	}
	if class == nil || class.UID == "" || class.Name != tenant.Spec.ProviderClassRef ||
		class.Spec.ControllerName != directory.KeycloakControllerName {
		return errors.New("identity tenant provider class does not match this controller")
	}
	if tenant.Status.BoundProviderClassUID != "" && tenant.Status.BoundProviderClassUID != class.UID {
		return errors.New("provider class was recreated; explicit migration is required")
	}
	if class.Spec.AuthenticationPolicy.IssuerMode != pmidentityv1alpha1.IssuerModeDedicated {
		return errors.New("default keycloak provider currently requires dedicated issuers")
	}
	if tenant.Spec.AdoptionRef != "" {
		return errors.New("external adoption requires the coordinated migration protocol")
	}
	if !tenant.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, tenant, class)
	}
	if errs := pmidentityv1alpha1.ValidateProviderClass(class); len(errs) != 0 {
		return errs.ToAggregate()
	}
	if class.Status.ContractVersion != pmidentityv1alpha1.ContractVersion ||
		class.Status.ObservedGeneration != class.Generation || class.Status.ObservedConfigurationRevision == "" ||
		!meta.IsStatusConditionTrue(class.Status.Conditions, "Ready") {
		return errors.New("provider class contract and configuration must be current and ready")
	}
	if errs := pmidentityv1alpha1.ValidateTenant(tenant); len(errs) != 0 {
		return errs.ToAggregate()
	}
	previousStatus := tenant.DeepCopy().Status
	if !controllerutil.ContainsFinalizer(tenant, Finalizer) {
		controllerutil.AddFinalizer(tenant, Finalizer)
		if err := r.Tenants.Update(ctx, tenant); err != nil {
			return fmt.Errorf("persist provider finalizer: %w", err)
		}
	}
	secret, journal, err := r.load(ctx, tenant)
	if apierrors.IsNotFound(err) {
		if tenant.Status.BoundProviderClassUID != "" || tenant.Status.ExternalID != "" || len(tenant.Status.Clients) != 0 {
			return r.fail(ctx, tenant, errors.New("private inventory is missing; refusing to recreate or forget remote resources"))
		}
		secret, journal, err = r.create(ctx, tenant, class.UID)
	}
	if err != nil {
		return err
	}
	if journal.ClassUID != class.UID || journal.ProviderIdentity != r.ProviderIdentity || journal.Retained {
		return r.fail(ctx, tenant, errors.New("private inventory provider binding changed or was retained; explicit migration is required"))
	}
	if tenant.Status.BoundProviderClassUID == "" {
		tenant.Status.ControllerName = directory.KeycloakControllerName
		tenant.Status.BoundProviderClassUID = class.UID
		setReady(&tenant.Status.Conditions, tenant.Generation, false, "Reconciling", "Reconciling provider state")
		if err := r.Tenants.Status().Update(ctx, tenant); err != nil {
			return fmt.Errorf("persist provider binding before remote writes: %w", err)
		}
	}
	err = r.process(ctx, tenant, secret, journal)
	if err != nil {
		r.project(tenant, secret.Name, journal)
		setReady(&tenant.Status.Conditions, tenant.Generation, false, "ReconciliationFailed", err.Error())
		return errors.Join(err, r.Tenants.Status().Update(ctx, tenant))
	}
	r.project(tenant, secret.Name, journal)
	tenant.Status.ObservedGeneration = tenant.Generation
	tenant.Status.ObservedConfigurationRevision = class.Status.ObservedConfigurationRevision
	setReady(&tenant.Status.Conditions, tenant.Generation, true, "Provisioned", "Tenant, clients and credential outputs are current")
	if equality.Semantic.DeepEqual(previousStatus, tenant.Status) {
		return nil
	}
	return r.Tenants.Status().Update(ctx, tenant)
}

func (r *Reconciler) process(ctx context.Context, tenant *pmidentityv1alpha1.IdentityTenant, secret *corev1.Secret, journal *inventory) error {
	observed, err := r.Backend.EnsureTenant(ctx, journal.Realm, journal.Owner, tenant.Spec.RegistrationAllowed, journal.TenantObserved)
	if err != nil {
		return err
	}
	if tenant.Status.IssuerURL != "" && tenant.Status.IssuerURL != observed.Issuer {
		return errors.New("provider issuer changed; explicit identity migration is required")
	}
	tenant.Status.ExternalID = journal.Realm
	tenant.Status.IssuerURL = observed.Issuer
	tenant.Status.DiscoveryURL = observed.DiscoveryURL
	tenant.Status.RegistrationEndpoint = observed.RegistrationEndpoint
	tenant.Status.SubjectMode = pmidentityv1alpha1.SubjectModePublic
	if !journal.TenantObserved {
		journal.TenantObserved = true
		if err := r.save(ctx, secret, journal); err != nil {
			return err
		}
	}
	desired := make(map[string]pmidentityv1alpha1.OIDCClient, len(tenant.Spec.Clients))
	for _, client := range tenant.Spec.Clients {
		desired[client.Name] = client
	}
	// Inventory, not spec, is authoritative for cleanup and survives removal.
	for _, name := range slices.Sorted(maps.Keys(journal.Clients)) {
		if _, exists := desired[name]; exists {
			continue
		}
		state := journal.Clients[name]
		if state.Info.ClientID == "" {
			return fmt.Errorf("client %s registration outcome is ambiguous; cleanup requires operator recovery", name)
		}
		state.Phase = "Deleting"
		if err := r.save(ctx, secret, journal); err != nil {
			return err
		}
		err := r.Backend.DeleteClient(ctx, journal.Realm, state.Info)
		if clientreg.IsUnauthorized(err) {
			if err := r.recover(ctx, secret, journal, state); err != nil {
				return fmt.Errorf("recover client %s for deletion: %w", name, err)
			}
			err = r.Backend.DeleteClient(ctx, journal.Realm, state.Info)
		}
		if err != nil {
			return fmt.Errorf("delete removed client %s: %w", name, err)
		}
		if err := r.cleanupOutputs(ctx, journal.Owner, state, ""); err != nil {
			return err
		}
		delete(journal.Clients, name)
		if err := r.save(ctx, secret, journal); err != nil {
			return err
		}
	}
	for _, client := range tenant.Spec.Clients {
		if err := r.reconcileClient(ctx, tenant, secret, journal, client); err != nil {
			return fmt.Errorf("client %s: %w", client.Name, err)
		}
	}
	return nil
}

func (r *Reconciler) reconcileClient(ctx context.Context, tenant *pmidentityv1alpha1.IdentityTenant, secret *corev1.Secret, journal *inventory, desired pmidentityv1alpha1.OIDCClient) error {
	state, exists := journal.Clients[desired.Name]
	metadata := clientMetadata(desired)
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if !exists {
		state = &clientState{Phase: "RegistrationPending"}
		journal.Clients[desired.Name] = state
		// Once this write succeeds, a restart must not replay registration.
		if err := r.save(ctx, secret, journal); err != nil {
			return err
		}
		info, err := r.Backend.Register(ctx, journal.Realm, metadata)
		if err != nil {
			response, httpError := clientreg.IsHTTPError(err)
			if errors.Is(err, clientreg.ErrRegistrationNotAttempted) ||
				(httpError && response.StatusCode >= http.StatusBadRequest && response.StatusCode < http.StatusInternalServerError && response.StatusCode != http.StatusRequestTimeout) {
				delete(journal.Clients, desired.Name)
				return errors.Join(fmt.Errorf("registration rejected before creating a client: %w", err), r.save(ctx, secret, journal))
			}
			return fmt.Errorf("registration outcome requires recovery before retry: %w", err)
		}
		state.Info, state.AppliedHash, state.Phase = info, hash, "Registered"
		if err := r.save(ctx, secret, journal); err != nil {
			return err
		}
	} else if state.Info.ClientID == "" {
		return errors.New("registration outcome is ambiguous; refusing to repeat non-idempotent registration")
	}
	observed, err := r.Backend.Read(ctx, journal.Realm, state.Info)
	if clientreg.IsUnauthorized(err) {
		if err := r.recover(ctx, secret, journal, state); err != nil {
			return err
		}
		observed, err = r.Backend.Read(ctx, journal.Realm, state.Info)
	}
	if err != nil {
		return fmt.Errorf("read registered client: %w", err)
	}
	state.Info = observed
	if err := r.save(ctx, secret, journal); err != nil {
		return err
	}
	externalID, err := r.Backend.ExternalClientID(ctx, journal.Realm, state.Info.ClientID)
	if err != nil {
		return err
	}
	if state.ExternalID != "" && state.ExternalID != externalID {
		return errors.New("native client identity changed; refusing implicit readoption")
	}
	state.ExternalID = externalID
	if err := r.save(ctx, secret, journal); err != nil {
		return err
	}
	matches, err := r.Backend.MatchesMetadata(ctx, journal.Realm, state.Info, metadata)
	if err != nil {
		return err
	}
	if hash != state.AppliedHash || state.Phase == "Updating" || !matches {
		state.Phase = "Updating"
		if err := r.save(ctx, secret, journal); err != nil {
			return err
		}
		info, err := r.Backend.Update(ctx, journal.Realm, state.Info, metadata)
		if err != nil {
			return err
		}
		state.Info, state.AppliedHash = info, hash
		// Persist rotated management credentials before publishing client outputs.
		if err := r.save(ctx, secret, journal); err != nil {
			return err
		}
		matches, err := r.Backend.MatchesMetadata(ctx, journal.Realm, info, metadata)
		if err != nil {
			return err
		}
		if !matches {
			return errors.New("provider did not apply the requested client metadata")
		}
	}
	if desired.TokenEndpointAuthMethod != pmidentityv1alpha1.ClientTokenEndpointAuthMethodNone && state.Info.ClientSecret == "" {
		return errors.New("confidential client credentials are missing from the provider response")
	}
	outputName := ""
	if desired.CredentialsSecretRef != nil {
		outputName = desired.CredentialsSecretRef.Name
		if !slices.Contains(state.Outputs, outputName) {
			state.Outputs = append(state.Outputs, outputName)
			if err := r.save(ctx, secret, journal); err != nil {
				return err
			}
		}
		if err := r.writeOutput(ctx, tenant, journal.Owner, outputName, state.Info, desired.TokenEndpointAuthMethod); err != nil {
			return err
		}
	}
	if err := r.cleanupOutputs(ctx, journal.Owner, state, outputName); err != nil {
		return err
	}
	state.Phase, state.ObservedGeneration = "Ready", tenant.Generation
	return r.save(ctx, secret, journal)
}

func (r *Reconciler) recover(ctx context.Context, secret *corev1.Secret, journal *inventory, state *clientState) error {
	token, err := r.Backend.RecoverManagementToken(ctx, journal.Realm, state.Info.ClientID, state.ExternalID)
	if err != nil {
		return err
	}
	state.Info.RegistrationAccessToken = token
	return r.save(ctx, secret, journal)
}

func privateName(owner string) string {
	return fmt.Sprintf("identity-%x", sha256.Sum256([]byte(owner)))
}

func (r *Reconciler) load(ctx context.Context, tenant *pmidentityv1alpha1.IdentityTenant) (*corev1.Secret, *inventory, error) {
	owner := r.Cluster.String() + "/" + string(tenant.UID)
	secret := &corev1.Secret{}
	if err := r.Private.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: r.RuntimeNamespace, Name: privateName(owner)}, secret); err != nil {
		return nil, nil, err
	}
	var journal inventory
	if err := json.Unmarshal(secret.Data[journalKey], &journal); err != nil {
		return nil, nil, errors.New("private identity inventory is invalid; recovery is required")
	}
	if journal.Version != 1 || journal.Owner != owner || secret.Annotations[ownerAnnotation] != owner || journal.Clients == nil ||
		journal.Realm != tenant.Name {
		return nil, nil, errors.New("private identity inventory does not match the tenant")
	}
	for _, state := range journal.Clients {
		if state == nil {
			return nil, nil, errors.New("private identity inventory contains an invalid client")
		}
	}
	return secret, &journal, nil
}

func (r *Reconciler) create(ctx context.Context, tenant *pmidentityv1alpha1.IdentityTenant, classUID types.UID) (*corev1.Secret, *inventory, error) {
	owner := r.Cluster.String() + "/" + string(tenant.UID)
	journal := &inventory{
		Version: 1, Owner: owner, ProviderIdentity: r.ProviderIdentity, ClassUID: classUID,
		Realm: tenant.Name, Clients: map[string]*clientState{},
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return nil, nil, err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: privateName(owner), Namespace: r.RuntimeNamespace,
			Annotations: map[string]string{ownerAnnotation: owner},
		},
		Type: corev1.SecretTypeOpaque, Data: map[string][]byte{journalKey: data},
	}
	if err := r.Private.Create(ctx, secret); err != nil {
		return nil, nil, fmt.Errorf("persist private inventory before provider writes: %w", err)
	}
	return secret, journal, nil
}

func (r *Reconciler) save(ctx context.Context, secret *corev1.Secret, journal *inventory) error {
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if bytes.Equal(secret.Data[journalKey], data) {
		return nil
	}
	secret.Data[journalKey] = data
	if err := r.Private.Update(ctx, secret); err != nil {
		return fmt.Errorf("persist private provider inventory: %w", err)
	}
	return nil
}

func (r *Reconciler) writeOutput(ctx context.Context, tenant *pmidentityv1alpha1.IdentityTenant, owner, name string, info clientreg.ClientInformation, method pmidentityv1alpha1.ClientTokenEndpointAuthMethod) error {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: r.OutputNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Tenants, secret, func() error {
		if secret.ResourceVersion != "" && secret.Annotations[ownerAnnotation] != owner {
			return errors.New("client credential output is owned by another resource")
		}
		if secret.Annotations == nil {
			secret.Annotations = map[string]string{}
		}
		secret.Annotations[ownerAnnotation] = owner
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = map[string][]byte{"client_id": []byte(info.ClientID)}
		if method != pmidentityv1alpha1.ClientTokenEndpointAuthMethodNone {
			secret.Data["client_secret"] = []byte(info.ClientSecret)
		}
		return controllerutil.SetControllerReference(tenant, secret, r.Tenants.Scheme())
	})
	return err
}

func (r *Reconciler) cleanupOutputs(ctx context.Context, owner string, state *clientState, keep string) error {
	for _, name := range state.Outputs {
		if name == keep {
			continue
		}
		secret := &corev1.Secret{}
		if err := r.Tenants.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: r.OutputNamespace}, secret); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if secret.Annotations[ownerAnnotation] != owner {
			return errors.New("refusing to delete an unowned client credential output")
		}
		if err := r.Tenants.Delete(ctx, secret, ctrlruntimeclient.Preconditions{UID: &secret.UID, ResourceVersion: &secret.ResourceVersion}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	state.Outputs = nil
	if keep != "" {
		state.Outputs = []string{keep}
	}
	return nil
}

func (r *Reconciler) project(tenant *pmidentityv1alpha1.IdentityTenant, managementName string, journal *inventory) {
	oldConditions := map[string][]metav1.Condition{}
	for _, client := range tenant.Status.Clients {
		oldConditions[client.Name] = client.Conditions
	}
	tenant.Status.Clients = nil
	desired := map[string]bool{}
	for _, client := range tenant.Spec.Clients {
		desired[client.Name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(journal.Clients)) {
		state := journal.Clients[name]
		client := pmidentityv1alpha1.ManagedClient{
			Name: name, ClientID: state.Info.ClientID, ExternalID: state.ExternalID,
			RegistrationClientURI: state.Info.RegistrationClientURI,
			ObservedGeneration:    state.ObservedGeneration, DeletionPending: !desired[name],
			ManagementSecretRef: &corev1.LocalObjectReference{Name: managementName},
			Conditions:          oldConditions[name],
		}
		if len(state.Outputs) == 1 && state.Phase == "Ready" {
			client.CredentialsSecretRef = &corev1.SecretReference{Name: state.Outputs[0], Namespace: r.OutputNamespace}
		}
		setReady(&client.Conditions, tenant.Generation, state.Phase == "Ready" && desired[name] && state.ObservedGeneration == tenant.Generation,
			state.Phase, "Client provider lifecycle state")
		tenant.Status.Clients = append(tenant.Status.Clients, client)
	}
}

func (r *Reconciler) finalize(ctx context.Context, tenant *pmidentityv1alpha1.IdentityTenant, class *pmidentityv1alpha1.IdentityProviderClass) error {
	if !controllerutil.ContainsFinalizer(tenant, Finalizer) {
		return nil
	}
	secret, journal, err := r.load(ctx, tenant)
	cleanup := meta.FindStatusCondition(tenant.Status.Conditions, "ProviderCleanupComplete")
	if apierrors.IsNotFound(err) && (tenant.Status.BoundProviderClassUID == "" ||
		(cleanup != nil && cleanup.Status == metav1.ConditionTrue && cleanup.ObservedGeneration == tenant.Generation)) {
		controllerutil.RemoveFinalizer(tenant, Finalizer)
		return r.Tenants.Update(ctx, tenant)
	}
	if err != nil {
		return fmt.Errorf("cannot finalize without private inventory: %w", err)
	}
	if journal.ClassUID != class.UID || journal.ProviderIdentity != r.ProviderIdentity {
		return errors.New("cannot finalize through a different provider identity")
	}
	if tenant.Spec.DeletionPolicy == pmidentityv1alpha1.DeletionPolicyRetain {
		journal.Retained = true
		if err := r.save(ctx, secret, journal); err != nil {
			return err
		}
		for _, state := range journal.Clients {
			for _, name := range state.Outputs {
				output := &corev1.Secret{}
				if err := r.Tenants.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: r.OutputNamespace}, output); err != nil {
					if apierrors.IsNotFound(err) {
						continue
					}
					return err
				}
				if output.Annotations[ownerAnnotation] != journal.Owner {
					return errors.New("cannot retain unowned credential output")
				}
				if err := controllerutil.RemoveControllerReference(tenant, output, r.Tenants.Scheme()); err != nil {
					return err
				}
				if err := r.Tenants.Update(ctx, output); err != nil {
					return err
				}
			}
		}
	} else {
		if err := r.Backend.DeleteTenant(ctx, journal.Realm, journal.Owner); err != nil {
			return err
		}
		for _, state := range journal.Clients {
			if err := r.cleanupOutputs(ctx, journal.Owner, state, ""); err != nil {
				return err
			}
		}
		meta.SetStatusCondition(&tenant.Status.Conditions, metav1.Condition{
			Type: "ProviderCleanupComplete", Status: metav1.ConditionTrue,
			ObservedGeneration: tenant.Generation, Reason: "Removed",
			Message: "Owned provider resources and credential outputs have been removed",
		})
		if err := r.Tenants.Status().Update(ctx, tenant); err != nil {
			return err
		}
		if err := r.Private.Delete(ctx, secret, ctrlruntimeclient.Preconditions{UID: &secret.UID, ResourceVersion: &secret.ResourceVersion}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	controllerutil.RemoveFinalizer(tenant, Finalizer)
	return r.Tenants.Update(ctx, tenant)
}

func setReady(conditions *[]metav1.Condition, generation int64, ready bool, reason, message string) {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}

	meta.SetStatusCondition(conditions, metav1.Condition{
		Type: "Ready", Status: status, ObservedGeneration: generation,
		Reason: reason, Message: message,
	})
}

func (r *Reconciler) fail(ctx context.Context, tenant *pmidentityv1alpha1.IdentityTenant, err error) error {
	setReady(&tenant.Status.Conditions, tenant.Generation, false, "ReconciliationFailed", err.Error())
	return errors.Join(err, r.Tenants.Status().Update(ctx, tenant))
}
