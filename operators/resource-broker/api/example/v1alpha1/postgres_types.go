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
	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:rbac:groups=example.platform-mesh.io,resources=postgres,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=example.platform-mesh.io,resources=postgres/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=example.platform-mesh.io,resources=postgres/finalizers,verbs=update

// PostgresSpec defines the desired state of Postgres.
type PostgresSpec struct {
	// Tier of the instance, e.g. dev or production.
	// +optional
	Tier string `json:"tier,omitempty"`

	// Major version of the postgres server.
	// +optional
	Version string `json:"version,omitempty"`

	// Name of the initial database.
	// +optional
	Database string `json:"database,omitempty"`
}

// PostgresStatus defines the observed state of Postgres.
type PostgresStatus struct {
	// conditions represent the current state of the Postgres resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this Postgres.
	// +optional
	RelatedResources pmbrokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`

	// Status reports the provider-side availability of the Postgres.
	// +optional
	Status pmbrokerv1alpha1.Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=postgres

// Postgres is the Schema for the postgres API.
type Postgres struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Postgres
	// +required
	Spec PostgresSpec `json:"spec"`

	// status defines the observed state of Postgres
	// +optional
	Status PostgresStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// PostgresList contains a list of Postgres.
type PostgresList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Postgres `json:"items"`
}
