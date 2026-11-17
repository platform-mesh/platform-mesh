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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KeycloakProviderConfigurationSpec is an administrator-owned configuration.
// Secret references resolve only in the provider's runtime namespace.
type KeycloakProviderConfigurationSpec struct {
	// +kubebuilder:validation:MinLength=1
	AdminURL string `json:"adminUrl"`
	// +kubebuilder:validation:MinLength=1
	PublicURL string `json:"publicUrl"`
	// credentialsSecretRef contains client_id and client_secret for the master realm.
	CredentialsSecretRef corev1.LocalObjectReference `json:"credentialsSecretRef"`
	// caSecretRef optionally contains ca.crt for the provider transport.
	// +optional
	CASecretRef *corev1.LocalObjectReference `json:"caSecretRef,omitempty"`
	// outputNamespace is the namespace in each tenant's own KCP workspace for
	// consumer credentials. It does not select a different workspace.
	// +kubebuilder:default=default
	// +optional
	OutputNamespace string `json:"outputNamespace,omitempty"`
	// +optional
	RegistrationAllowed bool `json:"registrationAllowed,omitempty"`
	// SMTP contains non-secret Keycloak SMTP configuration.
	// +optional
	SMTP map[string]string `json:"smtp,omitempty"`
	// smtpSecretRef optionally supplies password for SMTP.
	// +optional
	SMTPSecretRef *corev1.LocalObjectReference `json:"smtpSecretRef,omitempty"`
	// +kubebuilder:default=28800
	// +kubebuilder:validation:Minimum=60
	// +optional
	AccessTokenLifespan int `json:"accessTokenLifespan,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// KeycloakProviderConfiguration configures the independently deployed default
// implementation of the identity.platform-mesh.io API.
type KeycloakProviderConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              KeycloakProviderConfigurationSpec `json:"spec"`
}

// +kubebuilder:object:root=true
type KeycloakProviderConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KeycloakProviderConfiguration `json:"items"`
}
