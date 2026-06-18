package v1alpha1

import (
	"github.com/platform-mesh/subroutines/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InviteSpec defines the desired state of Invite
type InviteSpec struct {
	// +kubebuilder:validation:Format=email
	// +kubebuilder:validation:Pattern="[a-zA-Z0-9!#$%&'*+/=?^_`{|}~.-]+@[a-zA-Z0-9-]+(\\.[a-zA-Z0-9-]+)*"
	Email string `json:"email"`
}

// InviteStatus defines the observed state of Invite.
type InviteStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// Invite is the Schema for the invites API
type Invite struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Invite
	// +required
	Spec InviteSpec `json:"spec"`

	// status defines the observed state of Invite
	// +optional
	Status InviteStatus `json:"status,omitempty,omitzero"`
}

// GetConditions implements conditions.ConditionAccessor.
func (in *Invite) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions implements conditions.ConditionAccessor.
func (in *Invite) SetConditions(c []metav1.Condition) {
	in.Status.Conditions = c
}

// +kubebuilder:object:root=true

// InviteList contains a list of Invite
type InviteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Invite `json:"items"`
}

var _ conditions.ConditionAccessor = &Invite{}

func init() {
	SchemeBuilder.Register(&Invite{}, &InviteList{})
}
