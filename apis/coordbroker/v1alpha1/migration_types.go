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

// TODO: move these into a specific application

// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=migrations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=migrations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=migrations/finalizers,verbs=update

// MigrationSpec defines the desired state of Migration.
type MigrationSpec struct {
	// From indicates the source resource to be migrated.
	// +required
	From MigrationRef `json:"from"`
	// To indicates the target resource to be migrated.
	// +required
	To MigrationRef `json:"to"`
}

// MigrationRef references a specific resource involved in the
// migration.
type MigrationRef struct {
	// GVK is the GroupVersionKind of the resource.
	// +required
	GVK metav1.GroupVersionKind `json:"gvk"`
	// Name is the name of the resource.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Namespace is the namespace of the resource.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// ClusterName is the name of the cluster where the resource resides.
	// +required
	// +kubebuilder:validation:MinLength=1
	ClusterName string `json:"clusterName,omitempty"`
}

// MigrationStatus defines the observed state of Migration.
type MigrationStatus struct {
	// state represents the current state of the Migration process.
	State MigrationState `json:"state,omitempty"`

	// conditions represent the current state of the Migration resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// id is a unique identifier for the migration.
	ID string `json:"id,omitempty"`

	// stage is the stage of the migration process, the value is the id of the current stage from the MigrationConfiguration.
	Stage string `json:"stage,omitempty"`
}

// MigrationState represents the state of a Migration process.
type MigrationState string

const (
	// MigrationStateUnknown is the default, empty state.
	MigrationStateUnknown MigrationState = ""
	// MigrationStatePending means that the migration has been picked up by a reconciler but has not started yet.
	MigrationStatePending MigrationState = "Pending"
	// MigrationStateInitialInProgress indicates that the initial
	// migration is in progress.
	MigrationStateInitialInProgress MigrationState = "InitialInProgress"
	// MigrationStateInitialCompleted indicates that the initial
	// migration has been completed and that resources for the consumer
	// can be switched.
	MigrationStateInitialCompleted MigrationState = "InitialCompleted"
	// MigrationStateCutoverInProgress indicates that the cutover
	// migration is in progress.
	MigrationStateCutoverInProgress MigrationState = "CutoverInProgress"
	// MigrationStateCutoverCompleted indicates that the cutover
	// migration has been completed successfully.
	MigrationStateCutoverCompleted MigrationState = "CutoverCompleted"
	// MigrationStateFailed indicates that the migration has failed.
	MigrationStateFailed MigrationState = "Failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Migration is the Schema for the migrations API.
type Migration struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Migration
	// +required
	Spec MigrationSpec `json:"spec"`

	// status defines the observed state of Migration
	// +optional
	Status MigrationStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// MigrationList contains a list of Migration.
type MigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Migration `json:"items"`
}
