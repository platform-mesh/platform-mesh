package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

type ProviderVisibilityPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderVisibilityPolicySpec   `json:"spec,omitempty"`
	Status ProviderVisibilityPolicyStatus `json:"status,omitempty"`
}

type ProviderVisibilityPolicySpec struct {
	AccountRef AccountRef `json:"accountRef"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	ProviderExports []ProviderExport `json:"providerExports"`
}

type ProviderVisibilityPolicyStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// TODO: add status fields
	ResolvedProviderExports []ResolvedProviderExport `json:"resolvedProviderExports,omitempty"`
}

type ResolvedProviderExport struct {
	ClusterPath    string   `json:"clusterPath"`
	ClusterID      string   `json:"clusterID"`
	APIExportNames []string `json:"apiExportNames"`
}

// TODO: this is the org ref, simple string enough?
type AccountRef struct {
	ClusterPath string `json:"clusterPath"`
}

type ProviderExport struct {
	ProviderRef    ProviderRef `json:"providerRef"`
	APIExportNames []string    `json:"apiExportNames"`
}

type ProviderRef struct {
	ClusterPath string `json:"clusterPath"`
}

// +kubebuilder:object:root=true

// ProviderVisibilityPolicyList contains a list of ProviderVisibilityPolicy.
type ProviderVisibilityPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderVisibilityPolicy `json:"items"`
}
