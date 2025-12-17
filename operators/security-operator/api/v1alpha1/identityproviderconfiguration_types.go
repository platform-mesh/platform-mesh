package v1alpha1

import (
	lifecycleapi "github.com/platform-mesh/golang-commons/controller/lifecycle/api"
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
	ClientID               string                     `json:"clientId"`
	ClientName             string                     `json:"clientName"`
	RedirectURIs           []string                   `json:"redirectUris"`
	PostLogoutRedirectURIs []string                   `json:"postLogoutRedirectUris,omitempty"`
	// +optional
	SecretRef             corev1.SecretReference `json:"secretRef"`
	RegistrationClientURI string                 `json:"registrationClientUri,omitempty"`
}

// IdentityProviderConfigurationSpec defines the desired state of IdentityProviderConfiguration
type IdentityProviderConfigurationSpec struct {
	Clients []IdentityProviderClientConfig `json:"clients"`
}

// ManagedClient tracks a client that is managed by the operator.
type ManagedClient struct {
	ClientID              string                 `json:"clientId"`
	RegistrationClientURI string                 `json:"registrationClientUri"`
	SecretRef             corev1.SecretReference `json:"secretRef"`
}

// IdentityProviderConfigurationStatus defines the observed state of IdentityProviderConfiguration.
type IdentityProviderConfigurationStatus struct {
	Conditions     []metav1.Condition       `json:"conditions,omitempty"`
	ManagedClients map[string]ManagedClient `json:"managedClients,omitempty"`
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

// GetConditions implements api.RuntimeObjectConditions.
func (in *IdentityProviderConfiguration) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions implements api.RuntimeObjectConditions.
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

var _ lifecycleapi.RuntimeObjectConditions = &IdentityProviderConfiguration{}

func init() {
	SchemeBuilder.Register(&IdentityProviderConfiguration{}, &IdentityProviderConfigurationList{})
}
