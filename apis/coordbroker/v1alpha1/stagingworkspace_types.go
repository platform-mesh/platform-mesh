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

// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=stagingworkspaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=stagingworkspaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=coord.broker.platform-mesh.io,resources=stagingworkspaces/finalizers,verbs=update

// StagingWorkspaceSpec defines the desired state of StagingWorkspace.
type StagingWorkspaceSpec struct {
	// ConsumerCluster is the logical cluster name of the consumer
	// workspace the staging workspace serves.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ConsumerCluster string `json:"consumerCluster"`

	// ProviderCluster is the logical cluster name of the provider
	// workspace whose APIExport is bound in the staging workspace.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ProviderCluster string `json:"providerCluster"`

	// APIExportName is the name of the provider's APIExport to bind in
	// the staging workspace.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	APIExportName string `json:"apiExportName"`
}

// StagingWorkspacePhase is the lifecycle phase of a StagingWorkspace.
type StagingWorkspacePhase string

const (
	// StagingWorkspacePhasePending indicates the staging workspace is being provisioned.
	StagingWorkspacePhasePending StagingWorkspacePhase = "Pending"
	// StagingWorkspacePhaseReady indicates the staging workspace is fully provisioned and
	// resources can be staged in it.
	StagingWorkspacePhaseReady StagingWorkspacePhase = "Ready"
	// StagingWorkspacePhaseTerminating indicates the staging workspace is being torn down because
	// no assignments reference it anymore.
	StagingWorkspacePhaseTerminating StagingWorkspacePhase = "Terminating"
)

// Condition types for StagingWorkspace.
const (
	// StagingWorkspaceConditionWorkspaceReady indicates whether the kcp
	// workspace backing the staging workspace exists and is ready.
	StagingWorkspaceConditionWorkspaceReady = "WorkspaceReady"

	// StagingWorkspaceConditionBindingReady indicates whether the
	// APIBinding to the provider's APIExport inside the staging workspace
	// is ready.
	StagingWorkspaceConditionBindingReady = "BindingReady"
)

// StagingWorkspaceStatus defines the observed state of StagingWorkspace.
type StagingWorkspaceStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase StagingWorkspacePhase `json:"phase,omitempty"`

	// ClusterName is the logical cluster name of the created staging
	// workspace. Set once the workspace exists.
	// +optional
	ClusterName string `json:"clusterName,omitempty"`

	// Conditions represent the current state of the StagingWorkspace.
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

// StagingWorkspace represents the kcp workspace in which the broker
// stages consumer resources to sync them between consumer and provider.
// One exists per unique (consumerCluster, providerCluster, apiExportName)
// tuple; all resources of the same consumer/provider/API share it.
type StagingWorkspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StagingWorkspaceSpec   `json:"spec,omitempty"`
	Status StagingWorkspaceStatus `json:"status,omitempty"`
}

// GetConditions returns the conditions of the StagingWorkspace.
func (stagingWorkspace *StagingWorkspace) GetConditions() []metav1.Condition {
	return stagingWorkspace.Status.Conditions
}

// SetConditions sets the conditions of the StagingWorkspace.
func (stagingWorkspace *StagingWorkspace) SetConditions(conditions []metav1.Condition) {
	stagingWorkspace.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// StagingWorkspaceList contains a list of StagingWorkspace.
type StagingWorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StagingWorkspace `json:"items"`
}
