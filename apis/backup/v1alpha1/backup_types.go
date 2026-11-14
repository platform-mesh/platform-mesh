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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Phase is the state of a Backup.
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseCompleted Phase = "Completed"
	PhaseFailed    Phase = "Failed"
)

// IsFinished is true for Completed and Failed.
func (p Phase) IsFinished() bool {
	return p == PhaseCompleted || p == PhaseFailed
}

// BackupSpec defines the desired state of Backup. It can not be changed after create.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec is immutable"
type BackupSpec struct {
	// StorageRef is the BackupStorage in the same namespace.
	// +required
	StorageRef corev1.LocalObjectReference `json:"storageRef"`
}

// EtcdSnapshot is the snapshot of one etcd cluster.
type EtcdSnapshot struct {
	// Name of the druid Etcd resource.
	Name string `json:"name"`
	// Task is the EtcdOpsTask that took the snapshot.
	// +optional
	Task string `json:"task,omitempty"`
	// Snapshot is the key of the full snapshot in the druid store.
	// +optional
	Snapshot string `json:"snapshot,omitempty"`
	// +optional
	Size int64 `json:"size,omitempty"`
}

// BackupStatus defines the observed state of Backup.
type BackupStatus struct {
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// +optional
	Message string `json:"message,omitempty"`

	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Archive is the key of the backup archive in the bucket.
	// +optional
	Archive string `json:"archive,omitempty"`

	// +optional
	Etcds []EtcdSnapshot `json:"etcds,omitempty"`

	// Secrets is the number of secrets in the backup.
	// +optional
	Secrets int `json:"secrets,omitempty"`

	// Certificates is the number of cert-manager Certificates and Issuers in the backup.
	// +optional
	Certificates int `json:"certificates,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func (b *Backup) GetConditions() []metav1.Condition {
	return b.Status.Conditions
}

func (b *Backup) SetConditions(conditions []metav1.Condition) {
	b.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Archive",type=string,JSONPath=`.status.archive`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Backup takes one backup of the kcp etcd and the kcp PKI when it is created.
type Backup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec BackupSpec `json:"spec"`

	// +optional
	Status BackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BackupList contains a list of Backup
type BackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Backup `json:"items"`
}
