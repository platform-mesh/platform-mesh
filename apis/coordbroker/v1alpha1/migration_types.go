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

// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=migrations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=migrations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=migrations/finalizers,verbs=update

// MigrationSpec defines the desired state of Migration.
type MigrationSpec struct {
	// Assignment is the name of the Assignment being migrated.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Assignment string `json:"assignment"`

	// Namespace is the namespace of the migrated resource in the
	// staging workspaces. Empty for cluster-scoped resources.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name is the name of the migrated resource in the staging
	// workspaces.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// FromStagingWorkspace is the origin StagingWorkspace the resource
	// is migrated away from.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	FromStagingWorkspace string `json:"fromStagingWorkspace"`

	// StagingWorkspace is the destination StagingWorkspace the resource
	// is migrated to.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	StagingWorkspace string `json:"stagingWorkspace"`

	// From is the provider the resource is migrated away from.
	// +kubebuilder:validation:Required
	From MigrationTarget `json:"from"`

	// To is the provider the resource is migrated to.
	// +kubebuilder:validation:Required
	To MigrationTarget `json:"to"`
}

// MigrationTarget identifies a provider-side representation of the migrated resource.
type MigrationTarget struct {
	// GVK is the GroupVersionKind of the resource at this provider.
	// +kubebuilder:validation:Required
	GVK metav1.GroupVersionKind `json:"gvk"`

	// ProviderCluster is the logical cluster name of the provider workspace.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ProviderCluster string `json:"providerCluster"`

	// AcceptAPIName is the name of the AcceptAPI object in the provider cluster.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	AcceptAPIName string `json:"acceptAPIName"`
}

// Condition types recorded in [MigrationStatus.Conditions].
const (
	// MigrationConditionStagesCompleted tracks progress through the stages of the matching MigrationConfiguration.
	MigrationConditionStagesCompleted = "StagesCompleted"
	// MigrationConditionCutoverCompleted tracks whether the destination copy became available for cutover.
	MigrationConditionCutoverCompleted = "CutoverCompleted"
	// MigrationConditionReady aggregates the other conditions.
	MigrationConditionReady = "Ready"
)

// MigrationStatus defines the observed state of Migration.
type MigrationStatus struct {
	// State represents the current state of the migration process.
	// +optional
	State MigrationState `json:"state,omitempty"`

	// Stage is the name of the current stage from the matching MigrationConfiguration.
	// +optional
	Stage string `json:"stage,omitempty"`

	// Conditions represent the current state of the Migration.
	// Condition types are StagesCompleted, CutoverCompleted and Ready.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
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
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="From",type="string",JSONPath=".spec.from.providerCluster"
// +kubebuilder:printcolumn:name="To",type="string",JSONPath=".spec.to.providerCluster"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Migration moves a brokered resource from one provider to another.
type Migration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MigrationSpec   `json:"spec,omitempty"`
	Status MigrationStatus `json:"status,omitempty"`
}

// GetConditions returns the conditions of the Migration.
func (migration *Migration) GetConditions() []metav1.Condition {
	return migration.Status.Conditions
}

// SetConditions sets the conditions of the Migration.
func (migration *Migration) SetConditions(conditions []metav1.Condition) {
	migration.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// MigrationList contains a list of Migration.
type MigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Migration `json:"items"`
}
