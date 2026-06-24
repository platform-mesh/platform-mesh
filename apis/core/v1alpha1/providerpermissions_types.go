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
	"go.platform-mesh.io/subroutines/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProviderPermissionsSpec defines the desired state of ProviderPermissions.
type ProviderPermissionsSpec struct {
	// APIExportRef references the APIExport this configuration applies to
	APIExportRef APIExportReference `json:"apiExportRef"`

	// Roles defines custom roles grouped by resource type
	// +optional
	Roles []ResourceRoles `json:"roles,omitempty"`

	// Permissions defines per-resource permission configuration
	// Key format: {group}.{resource} (e.g., "orchestrate.platform-mesh.io.httpbin")
	// +optional
	Permissions map[string]ResourcePermissions `json:"permissions,omitempty"`
}

// APIExportReference identifies an APIExport resource.
type APIExportReference struct {
	// Name of the APIExport
	Name string `json:"name"`
}

// ResourceRoles defines roles for a specific resource type.
type ResourceRoles struct {
	// GroupResource identifies the resource type in format "{group}.{resource}"
	// (e.g., "orchestrate.platform-mesh.io.httpbin")
	GroupResource string `json:"groupResource"`

	// Roles defines the roles for this resource type
	Roles []RoleDefinition `json:"roles"`
}

// RoleDefinition defines a custom role with UI metadata and optional relation definition.
type RoleDefinition struct {
	// ID is the role identifier (must match relation name used in permissions)
	ID string `json:"id"`

	// DisplayName is the human-readable role name for UIs
	DisplayName string `json:"displayName"`

	// Description explains the role's purpose
	Description string `json:"description"`

	// Definition is the OpenFGA relation definition for this role
	// +optional
	Definition string `json:"definition,omitempty"`
}

// ResourcePermissions defines permissions for a specific resource type.
type ResourcePermissions struct {
	// DefaultPermissions overrides standard Kubernetes verb permissions
	// Empty string "" means use system default
	// +optional
	DefaultPermissions DefaultPermissions `json:"defaultPermissions,omitempty"`

	// AdditionalPermissions defines custom roles and custom permissions
	// +optional
	AdditionalPermissions map[string]string `json:"additionalPermissions,omitempty"`
}

// DefaultPermissions defines standard Kubernetes verb permissions.
type DefaultPermissions struct {
	// Get permission expression (empty string uses default)
	// +optional
	Get string `json:"get,omitempty"`

	// Update permission expression (empty string uses default)
	// +optional
	Update string `json:"update,omitempty"`

	// Delete permission expression (empty string uses default)
	// +optional
	Delete string `json:"delete,omitempty"`

	// Patch permission expression (empty string uses default)
	// +optional
	Patch string `json:"patch,omitempty"`

	// Watch permission expression (empty string uses default)
	// +optional
	Watch string `json:"watch,omitempty"`
}

// ProviderPermissionsStatus defines the observed state of ProviderPermissions.
type ProviderPermissionsStatus struct {
	// Conditions represent the latest available observations of the object's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation observed for this object
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ProviderPermissions allows API providers to customize authorization for their resources.
type ProviderPermissions struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderPermissionsSpec   `json:"spec,omitempty"`
	Status ProviderPermissionsStatus `json:"status,omitempty"`
}

// GetConditions implements conditions.ConditionAccessor.
func (in *ProviderPermissions) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions implements conditions.ConditionAccessor.
func (in *ProviderPermissions) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

var _ conditions.ConditionAccessor = &ProviderPermissions{}

// +kubebuilder:object:root=true

// ProviderPermissionsList contains a list of ProviderPermissions.
type ProviderPermissionsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderPermissions `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProviderPermissions{}, &ProviderPermissionsList{})
}
