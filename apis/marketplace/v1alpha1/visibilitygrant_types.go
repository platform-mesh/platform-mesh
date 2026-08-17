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

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

// VisibilityGrant defines the provider exports visible in a workspace subtree.
type VisibilityGrant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VisibilityGrantSpec `json:"spec,omitempty"`
}

// VisibilityGrantSpec defines the list of providers and their visible exports.
type VisibilityGrantSpec struct {
	Providers []ProviderExports `json:"providers"`
}

type ProviderExports struct {
	// ProviderClusterID is the provider's logical cluster ID.
	// +kubebuilder:validation:MinLength=1
	ProviderClusterID string `json:"providerClusterID"`

	// APIExports are the names of the visible provider APIExports.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	// +listType=set
	APIExports []string `json:"apiExports"`
}

// +kubebuilder:object:root=true

// VisibilityGrantList contains a list of VisibilityGrant.
type VisibilityGrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VisibilityGrant `json:"items"`
}
