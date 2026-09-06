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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IdPRegistrationSecretRef references a Secret in the same logical cluster as the
// IdPRegistration. Only the name is accepted — no namespace field to traverse.
type IdPRegistrationSecretRef struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// IdPRegistrationOIDCConfig holds tenant-supplied OIDC settings for an upstream IdP.
type IdPRegistrationOIDCConfig struct {
	// +kubebuilder:validation:Required
	ClientID string `json:"clientId"`

	// ClientSecret is a write-only convenience field accepted on create/update.
	// Admission stores the value in a Secret and sets clientSecretRef.name; it is
	// not persisted on the IdPRegistration.
	ClientSecret string `json:"clientSecret,omitempty"`

	ClientSecretRef IdPRegistrationSecretRef `json:"clientSecretRef"`

	// DiscoveryURL or manual endpoint fields (issuer, authorizationUrl, tokenUrl) are
	// mutually exclusive. Discovery is preferred when set.
	DiscoveryURL     string `json:"discoveryUrl,omitempty"`
	Issuer           string `json:"issuer,omitempty"`
	AuthorizationURL string `json:"authorizationUrl,omitempty"`
	TokenURL         string `json:"tokenUrl,omitempty"`
	JWKSURL          string `json:"jwksUrl,omitempty"`
}

// IdPRegistrationSpec defines the desired state of IdPRegistration.
type IdPRegistrationSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Alias string `json:"alias"`

	DisplayName     string              `json:"displayName,omitempty"`
	Enabled         *bool               `json:"enabled,omitempty"`
	HideOnLoginPage *bool               `json:"hideOnLoginPage,omitempty"`
	EmailDomainRouting *EmailDomainRouting `json:"emailDomainRouting,omitempty"`

	// +kubebuilder:validation:Enum=oidc
	// +kubebuilder:default=oidc
	Type UpstreamIdentityProviderType `json:"type"`

	// +kubebuilder:validation:Required
	OIDC *IdPRegistrationOIDCConfig `json:"oidc"`
}

// IdPRegistrationStatus defines the observed state of IdPRegistration.
type IdPRegistrationStatus struct {
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	Ready              bool               `json:"ready,omitempty"`
	OrganizationID     string             `json:"organizationId,omitempty"`
	LinkedEmailDomains []string           `json:"linkedEmailDomains,omitempty"`
	Message            string             `json:"message,omitempty"`
	// RedirectURI is the OAuth redirect URI tenants must allow at the upstream IdP.
	RedirectURI string `json:"redirectUri,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// IdPRegistration expresses an org owner's intent to configure an upstream OIDC IdP.
type IdPRegistration struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec IdPRegistrationSpec `json:"spec"`

	// +optional
	Status IdPRegistrationStatus `json:"status,omitempty,omitzero"`
}

// GetConditions implements conditions.ConditionAccessor.
func (in *IdPRegistration) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions implements conditions.ConditionAccessor.
func (in *IdPRegistration) SetConditions(c []metav1.Condition) {
	in.Status.Conditions = c
}

// +kubebuilder:object:root=true

// IdPRegistrationList contains a list of IdPRegistration.
type IdPRegistrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IdPRegistration `json:"items"`
}

var _ conditions.ConditionAccessor = &IdPRegistration{}
