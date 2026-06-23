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
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// MarketplaceEntrySpec defines the desired state of MarketplaceEntry.
type MarketplaceEntrySpec struct {

	// APIBindingName is the metadata.name of the APIBinding backing this installation.
	// Empty means not installed.
	APIBindingName string `json:"apiBindingName,omitempty"`

	// ProviderMetadata contains metadata about the provider of the marketplace entry.
	ProviderMetadata pmuiv1alpha1.ProviderMetadata `json:"providerMetadata"`

	// PermissionClaims are the permission claims associated with the marketplace entry.
	APIExport apisv1alpha1.APIExport `json:"apiExport"`
}

// MarketplaceEntryStatus defines the observed state of MarketplaceEntry.
type MarketplaceEntryStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// MarketplaceEntry is the Schema for the marketplaceentries API.
type MarketplaceEntry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MarketplaceEntrySpec   `json:"spec,omitempty"`
	Status MarketplaceEntryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MarketplaceEntryList contains a list of MarketplaceEntry.
type MarketplaceEntryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MarketplaceEntry `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion,
			&MarketplaceEntry{},
			&MarketplaceEntryList{},
		)
		metav1.AddToGroupVersion(s, GroupVersion)
		return nil
	})
}
