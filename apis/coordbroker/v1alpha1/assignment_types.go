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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=assignments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=assignments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=assignments/finalizers,verbs=update

// AssignmentSpec defines the desired state of Assignment.
type AssignmentSpec struct {
	// ConsumerCluster is the logical cluster name of the consumer
	// workspace the resource lives in.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ConsumerCluster string `json:"consumerCluster"`

	// GVR is the GroupVersionResource of the assigned resource.
	// +kubebuilder:validation:Required
	GVR metav1.GroupVersionResource `json:"gvr"`

	// Namespace is the namespace of the resource in the consumer
	// workspace. Empty for cluster-scoped resources.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name is the name of the resource in the consumer workspace.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// ProviderCluster is the logical cluster name of the provider
	// workspace the resource is assigned to.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ProviderCluster string `json:"providerCluster"`

	// AcceptAPIName is the name of the AcceptAPI object in the provider
	// workspace that accepted the resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	AcceptAPIName string `json:"acceptAPIName"`
}

// AssignmentPhase is the lifecycle phase of an Assignment.
type AssignmentPhase string

const (
	// AssignmentPhasePending indicates the staging workspace for the
	// assignment is being provisioned.
	AssignmentPhasePending AssignmentPhase = "Pending"
	// AssignmentPhaseBound indicates the staging workspace is ready and
	// the resource is synced through it.
	AssignmentPhaseBound AssignmentPhase = "Bound"
	// AssignmentPhaseTerminating indicates the assignment is being torn
	// down because the consumer resource was deleted.
	AssignmentPhaseTerminating AssignmentPhase = "Terminating"
)

// AssignmentStatus defines the observed state of Assignment.
type AssignmentStatus struct {
	// APIExportName is the name of the APIExport serving the assigned
	// GVR, resolved from the AcceptAPI once and kept for the lifetime
	// of the assignment.
	// +optional
	APIExportName string `json:"apiExportName,omitempty"`

	// StagingWorkspace is the name of the StagingWorkspace object the
	// assignment is served through.
	// +optional
	StagingWorkspace string `json:"stagingWorkspace,omitempty"`

	// Phase is the current lifecycle phase.
	// +optional
	Phase AssignmentPhase `json:"phase,omitempty"`

	// Conditions represent the current state of the Assignment.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.providerCluster"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Assignment records the sticky assignment of a single consumer resource
// to a provider. The broker creates one per brokered resource.
type Assignment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AssignmentSpec   `json:"spec,omitempty"`
	Status AssignmentStatus `json:"status,omitempty"`
}

// GetConditions returns the conditions of the Assignment.
func (assignment *Assignment) GetConditions() []metav1.Condition {
	return assignment.Status.Conditions
}

// SetConditions sets the conditions of the Assignment.
func (assignment *Assignment) SetConditions(conditions []metav1.Condition) {
	assignment.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// AssignmentList contains a list of Assignment.
type AssignmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Assignment `json:"items"`
}
