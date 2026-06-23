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

// SearchIndexSpec defines the desired state of SearchIndex
type SearchIndexSpec struct {
	// IndexPrefix is prepended to all index names for this workspace
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9-]*$`
	// +required
	IndexPrefix string `json:"indexPrefix"`

	// OrganizationClusterID immutable kcp cluster ID (LogicalCluster's kcp.io/cluster annotation); used as OpenSearch index name
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	// +required
	OrganizationClusterID string `json:"organizationClusterID"`

	// +kubebuilder:default=1
	NumberOfShards int32 `json:"numberOfShards"`

	// +kubebuilder:default=1
	NumberOfReplicas int32 `json:"numberOfReplicas"`

	// SemanticFields lists field paths to be used for semantic / vector search.
	// +optional
	SemanticFields []string `json:"semanticFields,omitempty"`

	// FilterableFields lists field paths to be used as exact-match filter facets.
	// +optional
	FilterableFields []string `json:"filterableFields,omitempty"`

	// DefaultFields lists all field paths derived from the APIResourceSchemas of
	// bound APIExports. Every field present in a custom resource schema is reflected here.
	// +optional
	DefaultFields []string `json:"defaultFields,omitempty"`

	// Paused stops all indexing when true
	// +optional
	Paused bool `json:"paused,omitempty"`
}

// SearchIndexStatus defines the observed state of SearchIndex.
type SearchIndexStatus struct {
	// IndexName is the OpenSearch index name
	// +optional
	IndexName string `json:"indexName,omitempty"`

	// DocumentCount is the number of documents indexed
	// +optional
	DocumentCount int64 `json:"documentCount,omitempty"`

	// LastSyncTime is the last successful sync time
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Conditions represent the current state of the SearchIndex resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetConditions returns the conditions for lifecycle manager compatibility
func (s *SearchIndex) GetConditions() []metav1.Condition {
	return s.Status.Conditions
}

// SetConditions sets the conditions for lifecycle manager compatibility
func (s *SearchIndex) SetConditions(conditions []metav1.Condition) {
	s.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Index",type=string,JSONPath=`.status.indexName`
// +kubebuilder:printcolumn:name="Documents",type=integer,JSONPath=`.status.documentCount`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// SearchIndex configures search indexing for a workspace
type SearchIndex struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of SearchIndex
	// +required
	Spec SearchIndexSpec `json:"spec"`

	// Status defines the observed state of SearchIndex
	// +optional
	Status SearchIndexStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SearchIndexList contains a list of SearchIndex
type SearchIndexList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SearchIndex `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SearchIndex{}, &SearchIndexList{})
}
