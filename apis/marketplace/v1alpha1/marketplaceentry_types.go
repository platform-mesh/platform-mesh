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
	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

// MarketplaceEntrySpec defines the desired state of MarketplaceEntry.
type MarketplaceEntrySpec struct {

	// APIBindingName is the metadata.name of the APIBinding backing this installation.
	// Empty means not installed.
	APIBindingName string `json:"apiBindingName,omitempty"`

	// ProviderMetadata contains metadata about the provider of the marketplace entry.
	ProviderMetadata pmuiv1alpha1.ProviderMetadata `json:"providerMetadata"`

	// APIExport is the v1alpha1-compatible export associated with the marketplace entry.
	// It is retained for existing Marketplace clients.
	APIExport LegacyAPIExport `json:"apiExport"`

	// APIExportPermissionClaims contains v1alpha2 permission claims associated with
	// the APIExport, including verbs and default selectors. Claims originating from
	// v1alpha1 resourceSelector are omitted because they cannot be translated to a
	// v1alpha2 label selector without broadening their scope.
	// +optional
	APIExportPermissionClaims []kcpapisv1alpha2.PermissionClaim `json:"apiExportPermissionClaims,omitempty"`
}

// LegacyAPIExport is the JSON-compatible v1alpha1 APIExport projection exposed
// to existing Marketplace clients. It uses a schema-safe permission-claim type
// because the deprecated SDK type has an optional list-map key without a
// default, which Kubernetes rejects when this API is persisted as a CRD.
type LegacyAPIExport struct {
	APIVersion string            `json:"apiVersion,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Metadata   metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state.
	// +optional
	Spec LegacyAPIExportSpec `json:"spec,omitempty"`

	// Status communicates the observed state.
	// +optional
	Status kcpapisv1alpha1.APIExportStatus `json:"status,omitempty"`
}

// LegacyAPIExportSpec is the JSON-compatible v1alpha1 APIExport spec.
type LegacyAPIExportSpec struct {
	// LatestResourceSchemas records the latest APIResourceSchemas exposed by the APIExport.
	// +optional
	// +listType=set
	LatestResourceSchemas []string `json:"latestResourceSchemas,omitempty"`

	// Identity points to the Secret containing the API identity.
	// +optional
	Identity *kcpapisv1alpha1.Identity `json:"identity,omitempty"`

	// MaximalPermissionPolicy sets the APIExport's upper authorization bound.
	// +optional
	MaximalPermissionPolicy *kcpapisv1alpha1.MaximalPermissionPolicy `json:"maximalPermissionPolicy,omitempty"`

	// PermissionClaims are the legacy v1alpha1 claims retained for existing clients.
	// +optional
	// +listType=map
	// +listMapKey=group
	// +listMapKey=resource
	PermissionClaims []LegacyPermissionClaim `json:"permissionClaims,omitempty"`
}

// LegacyPermissionClaim is the JSON-compatible v1alpha1 permission claim.
//
// +kubebuilder:validation:XValidation:rule="(has(self.all) && self.all) != (has(self.resourceSelector) && size(self.resourceSelector) > 0)",message="either \"all\" or \"resourceSelector\" must be set"
type LegacyPermissionClaim struct {
	// Group is the name of an API group. The empty string represents the core group.
	// +kubebuilder:validation:Pattern=`^(|[a-z0-9]([-a-z0-9]*[a-z0-9](\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*)?)$`
	// +kubebuilder:default:=""
	// +optional
	Group string `json:"group,omitempty"`

	// Resource is the name of the resource.
	// +kubebuilder:validation:Pattern=`^[a-z][-a-z0-9]*[a-z0-9]$`
	// +required
	// +kubebuilder:validation:Required
	Resource string `json:"resource"`

	// All claims all resources for the group/resource.
	// +optional
	All bool `json:"all,omitempty"`

	// ResourceSelector is the list of specifically claimed resources.
	// +optional
	ResourceSelector []kcpapisv1alpha1.ResourceSelector `json:"resourceSelector,omitempty"`

	// IdentityHash identifies the API identity for non-core resources.
	// +kubebuilder:default:=""
	// +optional
	IdentityHash string `json:"identityHash,omitempty"`
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
