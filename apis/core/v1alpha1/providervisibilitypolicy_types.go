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
	lifecycleapi "go.platform-mesh.io/golang-commons/controller/lifecycle/api"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

type ProviderVisibilityPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderVisibilityPolicySpec   `json:"spec,omitempty"`
	Status ProviderVisibilityPolicyStatus `json:"status,omitempty"`
}

// GetConditions implements [api.RuntimeObjectConditions].
func (in *ProviderVisibilityPolicy) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions implements [api.RuntimeObjectConditions].
func (in *ProviderVisibilityPolicy) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

type ProviderVisibilityPolicySpec struct {
	AccountRef AccountRef `json:"accountRef"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	ProviderExports []ProviderExport `json:"providerExports"`
}

type ProviderVisibilityPolicyStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// AccountClusterID is the logical cluster ID of the account allowed in the policy.
	AccountClusterID        string                   `json:"accountClusterID,omitempty"`
	ResolvedProviderExports []ResolvedProviderExport `json:"resolvedProviderExports,omitempty"`
}

type ResolvedProviderExport struct {
	ClusterPath    string   `json:"clusterPath"`
	ClusterID      string   `json:"clusterID"`
	APIExportNames []string `json:"apiExportNames"`
}

// TODO: this is the org ref, simple string enough?
type AccountRef struct {
	ClusterPath string `json:"clusterPath"`
}

type ProviderExport struct {
	ProviderRef    ProviderRef `json:"providerRef"`
	APIExportNames []string    `json:"apiExportNames"`
}

type ProviderRef struct {
	ClusterPath string `json:"clusterPath"`
}

var _ lifecycleapi.RuntimeObjectConditions = &ProviderVisibilityPolicy{}

// +kubebuilder:object:root=true

// ProviderVisibilityPolicyList contains a list of ProviderVisibilityPolicy.
type ProviderVisibilityPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderVisibilityPolicy `json:"items"`
}
