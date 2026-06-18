package v1alpha1

import (
	lifecycleapi "github.com/platform-mesh/golang-commons/controller/lifecycle/api"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type APIExportRef struct {
	Name        string `json:"name"`
	ClusterPath string `json:"clusterPath"`
}

type APIExportPolicySpec struct {
	APIExportRef APIExportRef `json:"apiExportRef"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	AllowPathExpressions []string `json:"allowPathExpressions"`
}

type APIExportPolicyStatus struct {
	Conditions              []metav1.Condition `json:"conditions,omitempty"`
	ManagedAllowExpressions []string           `json:"managedAllowExpressions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

type APIExportPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   APIExportPolicySpec   `json:"spec,omitempty"`
	Status APIExportPolicyStatus `json:"status,omitempty"`
}

// GetConditions implements lifecycle.RuntimeObjectConditions.
func (in *APIExportPolicy) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions implements lifecycle.RuntimeObjectConditions.
func (in *APIExportPolicy) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

var _ lifecycleapi.RuntimeObjectConditions = &APIExportPolicy{}

// +kubebuilder:object:root=true

// APIExportPolicyList contains a list of APIExportPolicy.
type APIExportPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []APIExportPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&APIExportPolicy{}, &APIExportPolicyList{})
}
