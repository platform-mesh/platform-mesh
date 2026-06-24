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

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,shortName=ca

// ClusterAccess is the Schema for the clusteraccesses API
type ClusterAccess struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterAccessSpec   `json:"spec,omitempty"`
	Status ClusterAccessStatus `json:"status,omitempty"`
}

// ClusterAccessSpec defines the desired state of ClusterAccess
// +kubebuilder:validation:XValidation:rule="has(self.host) || (has(self.auth) && has(self.auth.kubeconfigSecretRef))",message="host is required unless kubeconfigSecretRef auth is used"
type ClusterAccessSpec struct {
	// Path is an optional field. If not set, the name of the resource is used
	// +optional
	Path string `json:"path,omitempty"`

	// Host is the URL for the cluster. If not set and kubeconfig auth is used, the host from the kubeconfig is used.
	// +optional
	Host string `json:"host,omitempty"`

	// IntrospectionPath is an optional path suffix appended to Host when the listener
	// performs API discovery/introspection (e.g. "/clusters/*").
	// +optional
	IntrospectionPath string `json:"introspectionPath,omitempty"`

	// RequestPathTemplate defines a URL path template prepended to upstream API requests
	// at gateway time. The {clusterTarget} variable is resolved from the GraphQL extensions
	// field at request time. Example: "/clusters/{clusterTarget}"
	// +optional
	RequestPathTemplate string `json:"requestPathTemplate,omitempty"`

	// CA configuration for the cluster
	// +optional
	CA *CAConfig `json:"ca,omitempty"`

	// Auth configuration for the cluster
	// +optional
	Auth *AuthConfig `json:"auth,omitempty"`
}

// CAConfig defines CA configuration options
type CAConfig struct {
	// SecretRef points to a secret containing CA data
	// +optional
	SecretRef *SecretKeyRef `json:"secretRef,omitempty"`
}

// AuthConfig defines authentication configuration options
// +kubebuilder:validation:XValidation:rule="(has(self.tokenSecretRef) ? 1 : 0) + (has(self.kubeconfigSecretRef) ? 1 : 0) + (has(self.clientCertificateRef) ? 1 : 0) + (has(self.serviceAccountRef) ? 1 : 0) <= 1",message="only one of tokenSecretRef, kubeconfigSecretRef, clientCertificateRef, or serviceAccountRef can be set"
type AuthConfig struct {
	// SecretRef points to a secret containing auth token
	// +optional
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
	// KubeconfigSecretRef points to a secret containing kubeconfig
	// +optional
	KubeconfigSecretRef *SecretKeyRef `json:"kubeconfigSecretRef,omitempty"`
	// ClientCertificateRef points to secrets containing client certificate and key for mTLS
	// Secret must contain tls.crt and tls.key keys.
	// +optional
	ClientCertificateRef *corev1.SecretReference `json:"clientCertificateRef,omitempty"`
	// ServiceAccountRef points to a service account for token generation
	// +optional
	ServiceAccountRef *ServiceAccountRef `json:"serviceAccountRef,omitempty"`
}

// SecretKeyRef defines a reference to a secret with a specific key.
type SecretKeyRef struct {
	corev1.SecretReference `json:",inline"`
	// Key is the key in the secret data which contains the token
	// +optional
	Key string `json:"key,omitempty"`
}

// ClusterAccessStatus defines the observed state of ClusterAccess.
type ClusterAccessStatus struct {
	// Conditions represent the latest available observations of the cluster access state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ServiceAccountRef defines a reference to a service account.
type ServiceAccountRef struct {
	Name            string           `json:"name"`
	Namespace       string           `json:"namespace"`
	Audience        []string         `json:"audience,omitempty"`
	TokenExpiration *metav1.Duration `json:"token_expiration,omitempty"`
}

// ClusterAccessList contains a list of ClusterAccess
// +kubebuilder:object:root=true
type ClusterAccessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterAccess `json:"items"`
}
