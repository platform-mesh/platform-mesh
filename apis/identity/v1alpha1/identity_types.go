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

package v1alpha1

import (
	"go.platform-mesh.io/subroutines/conditions"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// IdentityProviderObjectReference identifies an API object without a workspace
// path or an untyped URL.
type IdentityProviderObjectReference struct {
	// apiGroup is the API group of the referenced object.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	APIGroup string `json:"apiGroup"`

	// kind is the kind of the referenced object.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[A-Za-z][A-Za-z0-9]*$`
	Kind string `json:"kind"`

	// name is the name of the referenced object.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
}

// IssuerMode determines whether a provider class has a tenant-specific issuer
// or shares its issuer among tenants.
type IssuerMode string

const (
	IssuerModeDedicated IssuerMode = "Dedicated"
	IssuerModeShared    IssuerMode = "Shared"
)

// AuthenticationPolicy is the administrator-owned authentication policy for a
// provider class.
// +kubebuilder:validation:XValidation:rule="!has(self.groupClaim) || size(self.groupClaim) == 0 || (has(self.groupPrefix) && size(self.groupPrefix) > 0 && !self.groupPrefix.startsWith('system:'))",message="a group claim requires a nonempty group prefix outside the system: namespace"
// +kubebuilder:validation:XValidation:rule="!has(self.groupPrefix) || !self.groupPrefix.startsWith('system:')",message="system: group prefixes are reserved"
type AuthenticationPolicy struct {
	// issuerMode selects a dedicated or shared issuer.
	// +kubebuilder:validation:Enum=Dedicated;Shared
	IssuerMode IssuerMode `json:"issuerMode"`

	// groupClaim is the optional claim used to identify groups in tokens.
	// +optional
	GroupClaim string `json:"groupClaim,omitempty"`

	// groupPrefix is prepended to each group claim value. Consumers must also
	// reject resulting group names in the reserved system: namespace.
	// +optional
	GroupPrefix string `json:"groupPrefix,omitempty"`

	// allowedTenantClaims lists trusted claim names usable as tenant constraints.
	// +listType=set
	// +optional
	AllowedTenantClaims []string `json:"allowedTenantClaims,omitempty"`
}

// IdentityEndpointConfig is an administrator-configured service endpoint.
// The TLS Secret name is resolved in the installation's configured namespace.
type IdentityEndpointConfig struct {
	// url is the endpoint URL.
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`

	// tlsSecretRef optionally references a Secret containing the endpoint CA.
	// Its namespace is supplied by installation configuration.
	// +optional
	TLSSecretRef *corev1.LocalObjectReference `json:"tlsSecretRef,omitempty"`
}

// IdentityProviderClassSpec defines how an identity provider is administered.
// +kubebuilder:validation:XValidation:rule="self.controllerName == oldSelf.controllerName",message="controllerName is immutable"
// +kubebuilder:validation:XValidation:rule="self.configurationRef.apiGroup == oldSelf.configurationRef.apiGroup && self.configurationRef.kind == oldSelf.configurationRef.kind && self.configurationRef.name == oldSelf.configurationRef.name",message="configurationRef is immutable"
// +kubebuilder:validation:XValidation:rule="self.authenticationPolicy == oldSelf.authenticationPolicy",message="authenticationPolicy is immutable"
type IdentityProviderClassSpec struct {
	// controllerName identifies the controller responsible for this class.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	ControllerName string `json:"controllerName"`

	// configurationRef identifies a provider-specific configuration object.
	ConfigurationRef IdentityProviderObjectReference `json:"configurationRef"`

	// authenticationPolicy contains administrator-owned authentication settings.
	AuthenticationPolicy AuthenticationPolicy `json:"authenticationPolicy"`

	// directory configures an optional provider directory endpoint.
	// +optional
	Directory *IdentityEndpointConfig `json:"directory,omitempty"`

	// validation configures an optional provider validation endpoint.
	// +optional
	Validation *IdentityEndpointConfig `json:"validation,omitempty"`
}

// IdentityCapability describes a capability supported by an identity provider.
type IdentityCapability string

const (
	IdentityCapabilityOIDC                      IdentityCapability = "OIDC"
	IdentityCapabilityDynamicClientRegistration IdentityCapability = "DynamicClientRegistration"
	IdentityCapabilityClientManagement          IdentityCapability = "ClientManagement"
	IdentityCapabilityTenantProvisioning        IdentityCapability = "TenantProvisioning"
	IdentityCapabilityInvitations               IdentityCapability = "Invitations"
	IdentityCapabilityDirectory                 IdentityCapability = "Directory"
	IdentityCapabilitySelfRegistration          IdentityCapability = "SelfRegistration"
	IdentityCapabilityLogout                    IdentityCapability = "Logout"
	IdentityCapabilitySharedIssuer              IdentityCapability = "SharedIssuer"
)

// IdentityProviderClassStatus reports the observed provider capabilities.
type IdentityProviderClassStatus struct {
	// contractVersion is the provider's identity API contract version.
	// +optional
	ContractVersion string `json:"contractVersion,omitempty"`

	// observedConfigurationRevision identifies the applied provider configuration.
	// +optional
	ObservedConfigurationRevision string `json:"observedConfigurationRevision,omitempty"`

	// observedGeneration is the last spec generation processed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// capabilities lists the features supported by this provider class.
	// +listType=set
	// +optional
	Capabilities []IdentityCapability `json:"capabilities,omitempty"`

	// conditions represent the current state of the provider class.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// IdentityProviderClass defines an administrator-managed identity provider.
type IdentityProviderClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IdentityProviderClassSpec   `json:"spec"`
	Status IdentityProviderClassStatus `json:"status,omitempty"`
}

func (in *IdentityProviderClass) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

func (in *IdentityProviderClass) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// IdentityProviderClassList contains a list of IdentityProviderClass objects.
type IdentityProviderClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IdentityProviderClass `json:"items"`
}

// IdentityTenantPurpose describes the user-facing purpose of an identity tenant.
type IdentityTenantPurpose string

const (
	IdentityTenantPurposeWelcome      IdentityTenantPurpose = "Welcome"
	IdentityTenantPurposeOrganization IdentityTenantPurpose = "Organization"
)

// IdentityClientPurpose describes the intended use of an OIDC client.
type IdentityClientPurpose string

const (
	IdentityClientPurposePortal      IdentityClientPurpose = "Portal"
	IdentityClientPurposeCLI         IdentityClientPurpose = "CLI"
	IdentityClientPurposeApplication IdentityClientPurpose = "Application"
)

// ClientTokenEndpointAuthMethod is a supported OAuth token endpoint
// authentication method.
type ClientTokenEndpointAuthMethod string

const (
	ClientTokenEndpointAuthMethodNone              ClientTokenEndpointAuthMethod = "none"
	ClientTokenEndpointAuthMethodClientSecretBasic ClientTokenEndpointAuthMethod = "client_secret_basic"
	ClientTokenEndpointAuthMethodClientSecretPost  ClientTokenEndpointAuthMethod = "client_secret_post"
)

// OIDCClient describes a desired OAuth/OIDC client.
// +kubebuilder:validation:XValidation:rule="self.name == oldSelf.name && self.purpose == oldSelf.purpose",message="name and purpose are immutable"
// +kubebuilder:validation:XValidation:rule="self.tokenEndpointAuthMethod == 'none' || (has(self.credentialsSecretRef) && has(self.credentialsSecretRef.name) && size(self.credentialsSecretRef.name) > 0)",message="confidential clients require a credentials Secret"
type OIDCClient struct {
	// name is the stable key for this client within its tenant.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// displayName is the human-readable name shown to users.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// purpose describes the intended use of this client.
	// +kubebuilder:validation:Enum=Portal;CLI;Application
	Purpose IdentityClientPurpose `json:"purpose"`

	// redirectURIs lists exact callback URLs allowed for this client.
	// +optional
	RedirectURIs []string `json:"redirectUris,omitempty"`

	// postLogoutRedirectURIs lists exact URLs allowed after logout.
	// +optional
	PostLogoutRedirectURIs []string `json:"postLogoutRedirectUris,omitempty"`

	// grantTypes lists OAuth 2.0 grant types enabled for this client.
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	GrantTypes []string `json:"grantTypes"`

	// responseTypes lists OAuth 2.0 response types enabled for this client.
	// +listType=set
	// +optional
	ResponseTypes []string `json:"responseTypes,omitempty"`

	// scopes lists the OAuth scopes available to this client.
	// +listType=set
	// +optional
	Scopes []string `json:"scopes,omitempty"`

	// tokenEndpointAuthMethod selects how the client authenticates to the token endpoint.
	// +kubebuilder:validation:Enum=none;client_secret_basic;client_secret_post
	TokenEndpointAuthMethod ClientTokenEndpointAuthMethod `json:"tokenEndpointAuthMethod"`

	// credentialsSecretRef identifies the client credentials Secret in the
	// installation-managed namespace, not a user-selected workspace or namespace.
	// +optional
	CredentialsSecretRef *corev1.LocalObjectReference `json:"credentialsSecretRef,omitempty"`
}

// IdentityTenantSpec defines the desired identity configuration for a tenant.
// +kubebuilder:validation:XValidation:rule="self.providerClassRef == oldSelf.providerClassRef && self.purpose == oldSelf.purpose && ((!has(self.organization) && !has(oldSelf.organization)) || (has(self.organization) && has(oldSelf.organization) && self.organization == oldSelf.organization))",message="providerClassRef, purpose, and organization are immutable"
// +kubebuilder:validation:XValidation:rule="(!has(self.adoptionRef) && !has(oldSelf.adoptionRef)) || (has(self.adoptionRef) && has(oldSelf.adoptionRef) && self.adoptionRef == oldSelf.adoptionRef)",message="adoptionRef is immutable"
type IdentityTenantSpec struct {
	// providerClassRef is the name of the IdentityProviderClass to use.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	ProviderClassRef string `json:"providerClassRef"`

	// purpose selects the kind of tenant identity being provisioned.
	// +kubebuilder:validation:Enum=Welcome;Organization
	Purpose IdentityTenantPurpose `json:"purpose"`

	// organization is the associated organization name, when applicable.
	// +optional
	Organization string `json:"organization,omitempty"`

	// registrationAllowed controls whether new registrations are permitted.
	// +optional
	RegistrationAllowed *bool `json:"registrationAllowed,omitempty"`

	// clients are the OIDC clients managed for this tenant.
	// +listType=map
	// +listMapKey=name
	Clients []OIDCClient `json:"clients"`

	// deletionPolicy controls whether provider-side tenant resources are removed.
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Delete
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`

	// adoptionRef requests adoption of an existing provider tenant. Admission
	// policy is responsible for restricting who may set this field.
	// +optional
	AdoptionRef string `json:"adoptionRef,omitempty"`
}

// DeletionPolicy controls provider-side cleanup when a resource is deleted.
type DeletionPolicy string

const (
	DeletionPolicyDelete DeletionPolicy = "Delete"
	DeletionPolicyRetain DeletionPolicy = "Retain"
)

// TenantClaim is a claim name and the values resolved for a particular tenant.
type TenantClaim struct {
	// name is the claim name.
	Name string `json:"name"`

	// values are the tenant-specific claim values.
	// +listType=set
	Values []string `json:"values"`
}

// ManagedClient reports the provider-side identity client and its Secret references.
type ManagedClient struct {
	// name is the stable client key from the tenant spec.
	Name string `json:"name"`

	// clientID is the provider-assigned OAuth client identifier.
	// +optional
	ClientID string `json:"clientId,omitempty"`

	// externalID is the provider-assigned client identifier.
	// +optional
	ExternalID string `json:"externalId,omitempty"`

	// registrationClientURI is the provider's client management endpoint.
	// +optional
	RegistrationClientURI string `json:"registrationClientUri,omitempty"`

	// credentialsSecretRef points to the client Secret in the installation-managed
	// namespace of this tenant's own KCP workspace.
	// +optional
	CredentialsSecretRef *corev1.SecretReference `json:"credentialsSecretRef,omitempty"`

	// managementSecretRef points to the management credentials Secret in the
	// private runtime namespace.
	// +optional
	ManagementSecretRef *corev1.LocalObjectReference `json:"managementSecretRef,omitempty"`

	// conditions represent the current state of this client.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the last spec generation processed for this client.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// deletionPending indicates that provider-side cleanup is still in progress.
	// +optional
	DeletionPending bool `json:"deletionPending,omitempty"`
}

// SubjectMode identifies the OpenID Connect subject identifier type.
type SubjectMode string

const (
	SubjectModePublic   SubjectMode = "Public"
	SubjectModePairwise SubjectMode = "Pairwise"
)

// IdentityTenantStatus reports provider-resolved identity and client details.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.boundProviderClassUID) || (has(self.boundProviderClassUID) && self.boundProviderClassUID == oldSelf.boundProviderClassUID)",message="boundProviderClassUID cannot be changed once set"
type IdentityTenantStatus struct {
	// observedGeneration is the last spec generation processed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// controllerName identifies the controller currently managing this tenant.
	// +optional
	ControllerName string `json:"controllerName,omitempty"`

	// boundProviderClassUID identifies the exact provider class instance used.
	// A recreated class with the same name must not silently replace this binding.
	// +optional
	BoundProviderClassUID types.UID `json:"boundProviderClassUID,omitempty"`

	// observedConfigurationRevision identifies the applied provider configuration.
	// +optional
	ObservedConfigurationRevision string `json:"observedConfigurationRevision,omitempty"`

	// externalID is the provider-assigned tenant identifier.
	// +optional
	ExternalID string `json:"externalId,omitempty"`

	// issuerURL is the resolved OpenID Connect issuer URL.
	// +optional
	IssuerURL string `json:"issuerUrl,omitempty"`

	// discoveryURL is the resolved OpenID Connect discovery document URL.
	// +optional
	DiscoveryURL string `json:"discoveryUrl,omitempty"`

	// subjectMode describes whether subjects are public or pairwise.
	// +kubebuilder:validation:Enum=Public;Pairwise
	// +optional
	SubjectMode SubjectMode `json:"subjectMode,omitempty"`

	// registrationEndpoint is the resolved dynamic client registration endpoint.
	// +optional
	RegistrationEndpoint string `json:"registrationEndpoint,omitempty"`

	// tenantClaims contains provider-resolved tenant-specific claim constraints.
	// +listType=map
	// +listMapKey=name
	// +optional
	TenantClaims []TenantClaim `json:"tenantClaims,omitempty"`

	// clients contains provider-managed client details and Secret references.
	// +listType=map
	// +listMapKey=name
	// +optional
	Clients []ManagedClient `json:"clients,omitempty"`

	// conditions represent the current state of the tenant.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// IdentityTenant is the aggregate identity configuration for one tenant.
type IdentityTenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IdentityTenantSpec   `json:"spec"`
	Status IdentityTenantStatus `json:"status,omitempty"`
}

func (in *IdentityTenant) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

func (in *IdentityTenant) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// IdentityTenantList contains a list of IdentityTenant objects.
type IdentityTenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IdentityTenant `json:"items"`
}

// PrincipalReference identifies an authenticated principal. Email addresses
// are not principal identifiers and are intentionally not included here.
type PrincipalReference struct {
	// issuer is the principal's identity issuer.
	Issuer string `json:"issuer"`

	// subject is the issuer-scoped principal subject.
	Subject string `json:"subject"`
}

// InvitationPhase is the lifecycle state of an invitation.
type InvitationPhase string

const (
	InvitationPhaseProvisioned InvitationPhase = "Provisioned"
	InvitationPhaseDispatched  InvitationPhase = "Dispatched"
	InvitationPhaseAccepted    InvitationPhase = "Accepted"
	InvitationPhaseFailed      InvitationPhase = "Failed"
	InvitationPhaseExpired     InvitationPhase = "Expired"
)

// InvitationSpec describes an invitation for a tenant user.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="invitation spec is immutable"
type InvitationSpec struct {
	// email is the delivery address, not the identity of the eventual principal.
	// +kubebuilder:validation:Format=email
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=254
	Email string `json:"email"`

	// tenantRef is the safe name of the tenant; it is resolved using AccountInfo.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	TenantRef string `json:"tenantRef"`

	// clientName is the stable client key used for the invitation.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ClientName string `json:"clientName"`

	// expiresAt optionally sets the invitation expiration time.
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`
}

// InvitationStatus reports the provider-side invitation lifecycle.
type InvitationStatus struct {
	// observedGeneration is the last spec generation processed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// externalID is the provider-assigned invitation identifier.
	// +optional
	ExternalID string `json:"externalId,omitempty"`

	// principal is set after the invited user has an established identity.
	// +optional
	Principal *PrincipalReference `json:"principal,omitempty"`

	// phase is the current invitation lifecycle state.
	// +kubebuilder:validation:Enum=Provisioned;Dispatched;Accepted;Failed;Expired
	// +optional
	Phase InvitationPhase `json:"phase,omitempty"`

	// conditions represent the current state of the invitation.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// Invitation represents an invitation to join an identity tenant.
type Invitation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InvitationSpec   `json:"spec"`
	Status InvitationStatus `json:"status,omitempty"`
}

func (in *Invitation) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

func (in *Invitation) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// InvitationList contains a list of Invitation objects.
type InvitationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Invitation `json:"items"`
}

var (
	_ conditions.ConditionAccessor = &IdentityProviderClass{}
	_ conditions.ConditionAccessor = &IdentityTenant{}
	_ conditions.ConditionAccessor = &Invitation{}
)
