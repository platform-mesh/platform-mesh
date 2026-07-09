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

// APIResourceSchemaRef references an APIResourceSchema by name.
type APIResourceSchemaRef struct {
	// Name is the metadata.name of the APIResourceSchema this config applies to.
	// +required
	Name string `json:"name"`
}

// SearchConfigSpec defines provider-declared field indexing behavior for a single APIResourceSchema.
type SearchConfigSpec struct {
	// APIResourceSchemaRef identifies the APIResourceSchema this config applies to.
	// +required
	APIResourceSchemaRef APIResourceSchemaRef `json:"apiResourceSchemaRef"`

	// ExcludedFields lists field paths that should NOT be indexed at all.
	// These fields are removed from defaultFields, semanticFields, and filterableFields.
	// +optional
	ExcludedFields []string `json:"excludedFields,omitempty"`

	// SemanticFields lists field paths to be indexed for vector/semantic search.
	// These are typically human-readable text fields where meaning matters.
	// +optional
	SemanticFields []string `json:"semanticFields,omitempty"`

	// ExactFields lists field paths to be indexed as keyword for exact-match filtering/faceting.
	// These are typically identifiers, enums, or structured values.
	// +optional
	ExactFields []string `json:"exactFields,omitempty"`
}

// SearchConfigStatus defines the observed state of SearchConfig.
type SearchConfigStatus struct {
	// Conditions represent the current state of the SearchConfig resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetConditions returns the conditions for lifecycle manager compatibility.
func (s *SearchConfig) GetConditions() []metav1.Condition {
	return s.Status.Conditions
}

// SetConditions sets the conditions for lifecycle manager compatibility.
func (s *SearchConfig) SetConditions(conditions []metav1.Condition) {
	s.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Schema",type=string,JSONPath=`.spec.apiResourceSchemaRef.name`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// SearchConfig declares per-APIResourceSchema field indexing behavior for the search operator.
type SearchConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired field indexing configuration.
	// +required
	Spec SearchConfigSpec `json:"spec"`

	// Status defines the observed state of SearchConfig.
	// +optional
	Status SearchConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SearchConfigList contains a list of SearchConfig.
type SearchConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SearchConfig `json:"items"`
}
