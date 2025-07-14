package v1alpha1

import (
	"fmt"

	"github.com/openmfp/golang-commons/controller/lifecycle"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	StoreRefLabelKey = "fga.openmfp.org/store-name"
)

type Tuple struct {
	Object   string `json:"object"`
	Relation string `json:"relation"`
	User     string `json:"user"`
}

func (t Tuple) String() string {
	return fmt.Sprintf("%s@%s#%s", t.Object, t.Relation, t.User)
}

// StoreSpec defines the desired state of Store.
type StoreSpec struct {
	CoreModule string  `json:"coreModule"`
	Tuples     []Tuple `json:"tuples,omitempty"`
}

// StoreStatus defines the observed state of Store.
type StoreStatus struct {
	Conditions           []metav1.Condition `json:"conditions,omitempty"`
	StoreID              string             `json:"storeId,omitempty"`
	AuthorizationModelID string             `json:"authorizationModelId,omitempty"`
	ManagedTuples        []Tuple            `json:"managedTuples,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// Store is the Schema for the stores API.
type Store struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StoreSpec   `json:"spec,omitempty"`
	Status StoreStatus `json:"status,omitempty"`
}

// GetConditions implements lifecycle.RuntimeObjectConditions.
func (in *Store) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions implements lifecycle.RuntimeObjectConditions.
func (in *Store) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

var _ lifecycle.RuntimeObjectConditions = &Store{}

// +kubebuilder:object:root=true

// StoreList contains a list of Store.
type StoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Store `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Store{}, &StoreList{})
}
