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

// StagingWorkspaceSpec defines the desired state of StagingWorkspace.
type StagingWorkspaceSpec struct {
	// ConsumerCluster is the logical cluster name of the consumer workspace.
	// +kubebuilder:validation:Required
	ConsumerCluster string `json:"consumerCluster"`

	// ProviderPath is the kcp workspace path of the provider
	// (value of the kcp.io/path annotation on AcceptAPI objects).
	// +kubebuilder:validation:Required
	ProviderPath string `json:"providerPath"`

	// APIExportName is the name of the provider's APIExport to bind in the
	// staging workspace.
	// +kubebuilder:validation:Required
	APIExportName string `json:"apiExportName"`

	// WorkspaceTreeRoot is the kcp workspace path under which the staging
	// workspace will be created (e.g. "root:rb").
	// +kubebuilder:validation:Required
	WorkspaceTreeRoot string `json:"workspaceTreeRoot"`
}

// StagingWorkspacePhase is the lifecycle phase of a StagingWorkspace.
type StagingWorkspacePhase string

const (
	// StagingWorkspacePhasePending indicates the staging workspace is being provisioned.
	StagingWorkspacePhasePending StagingWorkspacePhase = "Pending"
	// StagingWorkspacePhaseReady indicates the staging workspace is fully provisioned and ready for use.
	StagingWorkspacePhaseReady StagingWorkspacePhase = "Ready"
	// StagingWorkspacePhaseFailed indicates the staging workspace could not be provisioned.
	StagingWorkspacePhaseFailed StagingWorkspacePhase = "Failed"
	// StagingWorkspacePhaseTerminating indicates the staging workspace is being torn down because
	// all resources that were using it have been deleted. No new resources will be routed to it.
	StagingWorkspacePhaseTerminating StagingWorkspacePhase = "Terminating"
)

// StagingWorkspaceStatus defines the observed state of StagingWorkspace.
type StagingWorkspaceStatus struct {
	// WorkspaceURL is the direct access URL of the created kcp staging
	// workspace. Set once the workspace reaches the Ready phase.
	// +optional
	WorkspaceURL string `json:"workspaceURL,omitempty"`

	// Phase is the current lifecycle phase.
	// +optional
	Phase StagingWorkspacePhase `json:"phase,omitempty"`

	// Conditions represent the current state of the StagingWorkspace.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// StagingWorkspace represents a per-consumer x provider staging kcp workspace.
// resource-broker creates one for each unique (consumer workspace, provider
// workspace) pair and uses it to write consumer CRs that the provider's
// api-syncagent can pick up via the APIExport virtual workspace.
type StagingWorkspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StagingWorkspaceSpec   `json:"spec,omitempty"`
	Status StagingWorkspaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// StagingWorkspaceList contains a list of StagingWorkspace.
type StagingWorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StagingWorkspace `json:"items"`
}
