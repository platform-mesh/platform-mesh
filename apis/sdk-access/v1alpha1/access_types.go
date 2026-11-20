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

// Phase values on AccessStatus.
const (
	// AccessPhaseActive means the delivered kubeconfig is being
	// maintained (SA token renewal, CA bundle rotation).
	AccessPhaseActive = "Active"

	// AccessPhaseRevoked means the current SA token has expired and
	// cannot be renewed (PM-side revocation, workspace deletion, etc.).
	// Reconcilers stop acting on Revoked Accesses — the kubeconfig
	// Secret is not updated and no further renewal is attempted.
	// Revoked is a terminal state in v1alpha1.
	AccessPhaseRevoked = "Revoked"
)

// AccessSpec defines the desired state of an Access.
type AccessSpec struct {
	// identityRef references the AccessIdentity, in the same namespace,
	// that admitted the /access handshake that produced this Access.
	// +required
	IdentityRef corev1.LocalObjectReference `json:"identityRef"`

	// kubeconfigSecretRef references the Secret, in the same namespace,
	// holding the delivered kubeconfig. The access SDK writes and
	// maintains this Secret.
	// +optional
	KubeconfigSecretRef *corev1.LocalObjectReference `json:"kubeconfigSecretRef,omitempty"`
}

// AccessStatus defines the observed state of an Access.
type AccessStatus struct {
	// phase summarises the delivered kubeconfig's lifecycle: Active or
	// Revoked.
	// +optional
	Phase string `json:"phase,omitempty"`

	// lastRefreshed is the time of the most recent successful SA-token
	// renewal against kcp. The access SDK's renewal loop updates this
	// after each TokenRequest; consumers treat an Access whose
	// lastRefreshed is far behind the SA-token TTL as stale.
	// +optional
	LastRefreshed *metav1.Time `json:"lastRefreshed,omitempty"`

	// conditions represent the current state of the Access.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the last generation reconciled by the
	// access SDK.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:JSONPath=".spec.identityRef.name",name="Identity",type=string
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string
// +kubebuilder:printcolumn:JSONPath=".metadata.creationTimestamp",name="Age",type=date

// Access is the ongoing binding to one PM instance workspace, created by
// the access SDK on a successful /access handshake against an
// AccessIdentity. It references the delivered kubeconfig Secret, which
// the access SDK then owns and maintains.
type Access struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Access.
	// +required
	Spec AccessSpec `json:"spec"`

	// status defines the observed state of Access.
	// +optional
	Status AccessStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AccessList contains a list of Access.
type AccessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Access `json:"items"`
}

func (a *Access) GetConditions() []metav1.Condition {
	return a.Status.Conditions
}

func (a *Access) SetConditions(conditions []metav1.Condition) {
	a.Status.Conditions = conditions
}
