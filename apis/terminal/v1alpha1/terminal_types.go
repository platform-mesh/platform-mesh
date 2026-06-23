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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TerminalPhase represents the current state of the terminal
type TerminalPhase string

const (
	TerminalPhasePending     TerminalPhase = "Pending"
	TerminalPhaseCreating    TerminalPhase = "Creating"
	TerminalPhaseReady       TerminalPhase = "Ready"
	TerminalPhaseFailed      TerminalPhase = "Failed"
	TerminalPhaseTerminating TerminalPhase = "Terminating"
)

// TerminalSpec defines the desired state of Terminal.
// The target workspace is derived from where this resource is created.
// The host configuration (namespace, image, timeouts) is controlled by the controller.
type TerminalSpec struct {
	// Reserved for future use. Currently all terminal configuration is driven by
	// controller arguments to ensure consistent behavior across the platform.
}

// TerminalStatus defines the observed state of Terminal
type TerminalStatus struct {
	// Phase represents the current phase of the terminal
	// +optional
	Phase TerminalPhase `json:"phase,omitempty"`

	// SessionID is a unique, non-guessable identifier for this terminal session.
	// Used in the HTTPRoute path to prevent unauthorized access.
	// +optional
	SessionID string `json:"sessionId,omitempty"`

	// CreatedBy stores the user identity (sub claim from OIDC token) who created this terminal.
	// Used to verify the connecting user matches the creator.
	// +optional
	CreatedBy string `json:"createdBy,omitempty"`

	// PodName is the name of the created terminal pod on the runtime cluster
	// +optional
	PodName string `json:"podName,omitempty"`

	// WorkspacePath is the resolved kcp workspace path where this terminal was created
	// +optional
	WorkspacePath string `json:"workspacePath,omitempty"`

	// Conditions represent the latest available observations of the terminal's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation observed for this resource
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// NextReconcileTime is the next scheduled reconciliation time
	// +optional
	NextReconcileTime metav1.Time `json:"nextReconcileTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:JSONPath=".status.workspacePath",name="Workspace",type=string
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string
// +kubebuilder:printcolumn:JSONPath=".status.podName",name="Pod",type=string
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Terminal is the Schema for the terminals API.
// Create a Terminal resource in a kcp workspace to get an interactive shell
// with kubectl configured to access that workspace.
type Terminal struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TerminalSpec   `json:"spec,omitempty"`
	Status TerminalStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TerminalList contains a list of Terminal
type TerminalList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Terminal `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Terminal{}, &TerminalList{})
}

// GetObservedGeneration returns the observed generation
func (t *Terminal) GetObservedGeneration() int64 { return t.Status.ObservedGeneration }

// SetObservedGeneration sets the observed generation
func (t *Terminal) SetObservedGeneration(g int64) { t.Status.ObservedGeneration = g }

// GetNextReconcileTime returns the next reconcile time
func (t *Terminal) GetNextReconcileTime() metav1.Time { return t.Status.NextReconcileTime }

// SetNextReconcileTime sets the next reconcile time
func (t *Terminal) SetNextReconcileTime(time metav1.Time) { t.Status.NextReconcileTime = time }

// GetConditions returns the conditions
func (t *Terminal) GetConditions() []metav1.Condition { return t.Status.Conditions }

// SetConditions sets the conditions
func (t *Terminal) SetConditions(conditions []metav1.Condition) { t.Status.Conditions = conditions }
