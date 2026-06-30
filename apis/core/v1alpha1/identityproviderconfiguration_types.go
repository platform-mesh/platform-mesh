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
)

type IdentityProviderClientType string

const (
	IdentityProviderClientTypeConfidential IdentityProviderClientType = "confidential"
	IdentityProviderClientTypePublic       IdentityProviderClientType = "public"
)

type IdentityProviderClientConfig struct {
	// +kubebuilder:validation:Enum=confidential;public
	ClientType             IdentityProviderClientType `json:"clientType"`
	ClientName             string                     `json:"clientName"`
	RedirectURIs           []string                   `json:"redirectUris"`
	PostLogoutRedirectURIs []string                   `json:"postLogoutRedirectUris,omitempty"`
	SecretRef              corev1.SecretReference     `json:"secretRef,omitempty"`
}

type UpstreamIdentityProviderType string

const (
	UpstreamIdentityProviderTypeOIDC UpstreamIdentityProviderType = "oidc"
)

// UpstreamIdentityProvider holds fields independent from a provider's type
// and references provider-specific information under OIDC.
type UpstreamIdentityProvider struct {
	Alias                string                       `json:"alias"`
	DisplayName          string                       `json:"displayName,omitempty"`
	Enabled              *bool                        `json:"enabled,omitempty"`
	HideOnLoginPage      *bool                        `json:"hideOnLoginPage,omitempty"`
	AccountLinkingOnly   *bool                        `json:"accountLinkingOnly,omitempty"`
	StoreTokens          *bool                        `json:"storeTokens,omitempty"`
	StoredTokensReadable *bool                        `json:"storedTokensReadable,omitempty"`
	TrustEmail           *bool                        `json:"trustEmail,omitempty"`
	GUIOrder             *int                         `json:"guiOrder,omitempty"`
	VerifyEssentialClaim *bool                        `json:"verifyEssentialClaim,omitempty"`
	EssentialClaim       string                       `json:"essentialClaim,omitempty"`
	EssentialClaimValue  string                       `json:"essentialClaimValue,omitempty"`
	FirstLoginFlow       string                       `json:"firstLoginFlow,omitempty"`
	PostLoginFlow        string                       `json:"postLoginFlow,omitempty"`
	// +kubebuilder:validation:Enum=legacy;import;force
	SyncMode string `json:"syncMode,omitempty"`
	CaseSensitiveUsername *bool `json:"caseSensitiveUsername,omitempty"`
	// +kubebuilder:validation:Enum=Always;WhenLinked;Never
	ShowInAccountConsole string                       `json:"showInAccountConsole,omitempty"`
	// +kubebuilder:validation:Enum=oidc
	Type UpstreamIdentityProviderType `json:"type"`
	OIDC *OIDCUpstreamConfig          `json:"oidc,omitempty"`
}

// OIDCUpstreamConfig holds OIDC-specific upstream identity provider
// configuration.
type OIDCUpstreamConfig struct {
	// Either DiscoveryURL or the manual endpoint fields need to be set.
	DiscoveryURL     string                 `json:"discoveryUrl,omitempty"`
	Issuer           string                 `json:"issuer,omitempty"`
	AuthorizationURL string                 `json:"authorizationUrl,omitempty"`
	TokenURL         string                 `json:"tokenUrl,omitempty"`
	LogoutURL        string                 `json:"logoutUrl,omitempty"`
	BackchannelLogout *bool                 `json:"backchannelLogout,omitempty"`
	UserInfoURL      string                 `json:"userInfoUrl,omitempty"`
	ClientAuthentication string             `json:"clientAuthentication,omitempty"`
	ClientID         string                 `json:"clientId,omitempty"`
	ClientSecretRef  corev1.SecretReference `json:"clientSecretRef,omitempty"`
	ClientAssertionSignatureAlgorithm string `json:"clientAssertionSignatureAlgorithm,omitempty"`
	ClientAssertionAudience         string `json:"clientAssertionAudience,omitempty"`
	DefaultScopes    string                 `json:"defaultScopes,omitempty"`
	Prompt           string                 `json:"prompt,omitempty"`
	AcceptsPromptNoneForwardFromClient *bool `json:"acceptsPromptNoneForwardFromClient,omitempty"`
	RequiresShortStateParameter        *bool `json:"requiresShortStateParameter,omitempty"`
	ValidateSignatures                 *bool `json:"validateSignatures,omitempty"`
	UseJWKSURL                         *bool `json:"useJwksUrl,omitempty"`
	JWKSURL                            string `json:"jwksUrl,omitempty"`
	ValidatingPublicKey                string `json:"validatingPublicKey,omitempty"`
	ValidatingPublicKeyID              string `json:"validatingPublicKeyId,omitempty"`
	ForwardedQueryParameters           string `json:"forwardedQueryParameters,omitempty"`
	SupportsClientAssertions           *bool  `json:"supportsClientAssertions,omitempty"`
	AllowsClientAssertionsReused       *bool  `json:"allowsClientAssertionsReused,omitempty"`
	AllowsClientIDAsAudienceForAssertions *bool `json:"allowsClientIdAsAudienceForAssertions,omitempty"`
}

// IdentityProviderConfigurationSpec defines the desired state of IdentityProviderConfiguration
type IdentityProviderConfigurationSpec struct {
	RegistrationAllowed       bool                           `json:"registrationAllowed,omitempty"`
	Clients                   []IdentityProviderClientConfig `json:"clients"`
	UpstreamIdentityProviders []UpstreamIdentityProvider     `json:"upstreamIdentityProviders,omitempty"`
}

// ManagedClient tracks a client that is managed by the operator.
type ManagedClient struct {
	ClientID              string                 `json:"clientId"`
	RegistrationClientURI string                 `json:"registrationClientUri"`
	SecretRef             corev1.SecretReference `json:"secretRef"`
}

// UpstreamIdentityProviderStatus tracks reconciliation of an upstream
// identity provider in Keycloak.
type UpstreamIdentityProviderStatus struct {
	Alias        string      `json:"alias"`
	Ready        bool        `json:"ready"`
	Message      string      `json:"message,omitempty"`
	LastSyncTime metav1.Time `json:"lastSyncTime,omitempty"`
}

// IdentityProviderConfigurationStatus defines the observed state of IdentityProviderConfiguration.
type IdentityProviderConfigurationStatus struct {
	Conditions                       []metav1.Condition                            `json:"conditions,omitempty"`
	ManagedClients                   map[string]ManagedClient                      `json:"managedClients,omitempty"`
	ManagedUpstreamIdentityProviders map[string]UpstreamIdentityProviderStatus     `json:"managedUpstreamIdentityProviders,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// IdentityProviderConfiguration is the Schema for the identityproviderconfigurations API
type IdentityProviderConfiguration struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of IdentityProviderConfiguration
	// +required
	Spec IdentityProviderConfigurationSpec `json:"spec"`

	// status defines the observed state of IdentityProviderConfiguration
	// +optional
	Status IdentityProviderConfigurationStatus `json:"status,omitempty,omitzero"`
}

// GetConditions implements conditions.ConditionAccessor.
func (in *IdentityProviderConfiguration) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions implements conditions.ConditionAccessor.
func (in *IdentityProviderConfiguration) SetConditions(c []metav1.Condition) {
	in.Status.Conditions = c
}

// +kubebuilder:object:root=true

// IdentityProviderConfigurationList contains a list of IdentityProviderConfiguration
type IdentityProviderConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IdentityProviderConfiguration `json:"items"`
}

var _ conditions.ConditionAccessor = &IdentityProviderConfiguration{}
