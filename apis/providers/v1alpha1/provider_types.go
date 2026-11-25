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

const (
	ProviderPhasePending                = "Pending"
	ProviderPhaseProvisioningWorkspace  = "ProvisioningWorkspace"
	ProviderPhaseProvisioningKubeconfig = "ProvisioningKubeconfig"
	ProviderPhaseReady                  = "Ready"
	ProviderPhaseDeleting               = "Deleting"
)

// ProviderSpec defines the desired state of Provider.
// Provider is a kcp-level resource that handles kcp-side bootstrap only
// (Workspace, ServiceAccount, RBAC, kubeconfig Secret). It has no runtime-side effects.
type ProviderSpec struct {
	// providerKubeconfigSecret is a Secret specification used to store
	// the provider's kubeconfig. This is the admin kubeconfig that provider controllers
	// can use to access the workspace in :root:providers:<WS name>.
	// If not provided, a default of Namespace:default, Name:<Provider.Name>-provider-kubeconfig,
	// Key:kubeconfig is used.
	//
	// +optional
	ProviderKubeconfigSecret *KubeconfigSecretSpec `json:"providerKubeconfigSecret,omitempty"`

	// hostOverride overrides the kcp front-proxy host written into the
	// generated kubeconfig Secret. Optional; defaults to the operator-configured
	// front-proxy URL.
	// +optional
	HostOverride string `json:"hostOverride,omitempty"`

	// authentication sets how the provider controllers log in to the provider workspace.
	// Not set means a ServiceAccount token in the kubeconfig Secret. It cannot change later.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="authentication is immutable"
	Authentication *ProviderAuthentication `json:"authentication,omitempty"`
}

// ProviderAuthentication sets how the provider controllers log in to the provider workspace.
type ProviderAuthentication struct {
	// oidc lets the provider controllers log in with tokens of their own issuer,
	// e.g. ServiceAccount tokens of their cluster. No credential is stored in Platform Mesh.
	// +optional
	OIDC *ProviderOIDCAuthentication `json:"oidc,omitempty"`
}

// ProviderOIDCAuthentication is a JWT issuer that kcp trusts for the provider workspace only.
type ProviderOIDCAuthentication struct {
	// issuerURL must match the "iss" claim of the tokens.
	// +required
	// +kubebuilder:validation:Pattern=`^https://`
	IssuerURL string `json:"issuerURL"`

	// discoveryURL is where kcp reads the discovery document, if not under issuerURL.
	// +optional
	DiscoveryURL string `json:"discoveryURL,omitempty"`

	// certificateAuthority is the PEM CA of the discovery and JWKS endpoints.
	// +optional
	CertificateAuthority string `json:"certificateAuthority,omitempty"`

	// audiences are the accepted values of the "aud" claim.
	// +required
	// +kubebuilder:validation:MinItems=1
	Audiences []string `json:"audiences"`

	// subjects are the "sub" claims with full access to the provider workspace,
	// e.g. system:serviceaccount:my-provider:controller.
	// +required
	// +kubebuilder:validation:MinItems=1
	Subjects []string `json:"subjects"`
}

// KubeconfigSecretSpec describes a Secret and the key with kubeconfig YAML content.
type KubeconfigSecretSpec struct {
	// name is the name of the Secret.
	Name string `json:"name"`

	// namespace is the namespace of the Secret.
	Namespace string `json:"namespace"`

	// key is the key in data map where the kubeconfig YAML content is stored.
	Key string `json:"key"`
}

// ProviderStatus defines the observed state of Provider.
type ProviderStatus struct {
	// phase summarises the bootstrap state of the Provider (e.g. "Pending", "Ready").
	// +optional
	Phase string `json:"phase,omitempty"`

	// providerKubeconfigSecretRef points to the Secret that contains
	// the scoped kubeconfig for the provider workspace.
	// +optional
	ProviderKubeconfigSecretRef *corev1.SecretReference `json:"providerKubeconfigSecretRef,omitempty"`

	// conditions represent the current state of the Provider resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the last generation reconciled by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string,description="Bootstrap phase"
// +kubebuilder:printcolumn:JSONPath=".status.kubeconfigSecretRef.name",name="KubeconfigSecret",type=string,description="Name of the generated kubeconfig Secret"
// +kubebuilder:printcolumn:JSONPath=".status.conditions[?(@.type=='Ready')].status",name="Ready",type=string,description="Shows if resource is ready"

// Provider is the Schema for the providers API.
// It is a kcp-level resource that bootstraps provider RBAC and credentials.
type Provider struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Provider
	// +required
	Spec ProviderSpec `json:"spec"`

	// status defines the observed state of Provider
	// +optional
	Status ProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderList contains a list of Provider
type ProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Provider `json:"items"`
}

func (i *Provider) GetConditions() []metav1.Condition {
	return i.Status.Conditions
}

func (i *Provider) SetConditions(conditions []metav1.Condition) {
	i.Status.Conditions = conditions
}
