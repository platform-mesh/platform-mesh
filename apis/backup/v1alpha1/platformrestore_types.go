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

// +kubebuilder:validation:Enum=ValidatingTopology;RestoringEtcd;RestoringCNPG;RestoringVelero;Repairing;Succeeded;Failed
type RestorePhase string

const (
	RestorePhaseValidatingTopology RestorePhase = "ValidatingTopology"
	RestorePhaseRestoringEtcd      RestorePhase = "RestoringEtcd"
	RestorePhaseRestoringCNPG      RestorePhase = "RestoringCNPG"
	RestorePhaseRestoringVelero    RestorePhase = "RestoringVelero"
	RestorePhaseRepairing          RestorePhase = "Repairing"
	RestorePhaseSucceeded          RestorePhase = "Succeeded"
	RestorePhaseFailed             RestorePhase = "Failed"
)

// +kubebuilder:validation:Enum=Strict
type TopologyValidationMode string

const TopologyValidationStrict TopologyValidationMode = "Strict"

// PlatformRestoreSpec defines the desired state of PlatformRestore
type PlatformRestoreSpec struct {
	Source             RestoreSourceSpec      `json:"source"`
	TopologyValidation TopologyValidationMode `json:"topologyValidation"`
	Repair             RepairSpec             `json:"repair,omitempty"`
}

type RestoreSourceSpec struct {
	Storage  StorageSpec `json:"storage"`
	BackupID string      `json:"backupID"`
}

type RepairSpec struct {
	Delete bool `json:"delete,omitempty"`
}

// PlatformRestoreStatus defines the observed state of PlatformRestore
type PlatformRestoreStatus struct {
	Phase              RestorePhase       `json:"phase,omitempty"`
	TopologyValidation TopologyValStatus  `json:"topologyValidation,omitempty"`
	Repair             RepairStatus       `json:"repair,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	NextReconcileTime  metav1.Time        `json:"nextReconcileTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

type TopologyValStatus struct {
	SourceDigest string `json:"sourceDigest,omitempty"`
	TargetDigest string `json:"targetDigest,omitempty"`
	Matches      bool   `json:"matches,omitempty"`
}

type RepairStatus struct {
	OrphanTuplesFound   int `json:"orphanTuplesFound,omitempty"`
	OrphanTuplesDeleted int `json:"orphanTuplesDeleted,omitempty"`
	OrphanTuplesDryRun  int `json:"orphanTuplesDryRun,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// PlatformRestore is the Schema for the platformrestores API
type PlatformRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlatformRestoreSpec   `json:"spec,omitempty"`
	Status PlatformRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PlatformRestoreList contains a list of PlatformRestore
type PlatformRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PlatformRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PlatformRestore{}, &PlatformRestoreList{})
}

func (r *PlatformRestore) GetConditions() []metav1.Condition  { return r.Status.Conditions }
func (r *PlatformRestore) SetConditions(c []metav1.Condition) { r.Status.Conditions = c }
func (r *PlatformRestore) GetObservedGeneration() int64       { return r.Status.ObservedGeneration }
func (r *PlatformRestore) SetObservedGeneration(g int64)      { r.Status.ObservedGeneration = g }
func (r *PlatformRestore) GetNextReconcileTime() metav1.Time  { return r.Status.NextReconcileTime }
func (r *PlatformRestore) SetNextReconcileTime(t metav1.Time) { r.Status.NextReconcileTime = t }
