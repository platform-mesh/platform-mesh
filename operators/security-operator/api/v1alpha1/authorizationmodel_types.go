package v1alpha1

import (
	lifecycleapi "github.com/platform-mesh/golang-commons/controller/lifecycle/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type WorkspaceStoreRef struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// AuthorizationModelSpec defines the desired state of AuthorizationModel.
type AuthorizationModelSpec struct {
	StoreRef WorkspaceStoreRef `json:"storeRef"`
	Model    string            `json:"model"`
	Tuples   []Tuple           `json:"tuples,omitempty"`
}

// AuthorizationModelStatus defines the observed state of AuthorizationModel.
type AuthorizationModelStatus struct {
	Conditions    []metav1.Condition `json:"conditions,omitempty"`
	ManagedTuples []Tuple            `json:"managedTuples,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// AuthorizationModel is the Schema for the authorizationmodels API.
type AuthorizationModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuthorizationModelSpec   `json:"spec,omitempty"`
	Status AuthorizationModelStatus `json:"status,omitempty"`
}

// GetConditions implements lifecycle.RuntimeObjectConditions.
func (in *AuthorizationModel) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions implements lifecycle.RuntimeObjectConditions.
func (in *AuthorizationModel) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

var _ lifecycleapi.RuntimeObjectConditions = &AuthorizationModel{}

// +kubebuilder:object:root=true

// AuthorizationModelList contains a list of AuthorizationModel.
type AuthorizationModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AuthorizationModel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AuthorizationModel{}, &AuthorizationModelList{})
}
