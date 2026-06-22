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
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ContentConfigurationSpec defines the desired state of ContentConfiguration
type ContentConfigurationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:validation:RemoteConfiguration:
	RemoteConfiguration *RemoteConfiguration `json:"remoteConfiguration,omitempty"`

	InlineConfiguration *InlineConfiguration `json:"inlineConfiguration,omitempty"`
}

type InlineConfiguration struct {
	// +kubebuilder:validation:Enum=yaml;json
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type RemoteConfiguration struct {
	// +kubebuilder:validation:Enum=yaml;json
	ContentType    string         `json:"contentType"`
	URL            string         `json:"url"`
	InternalUrl    string         `json:"internalUrl,omitempty"`
	Authentication Authentication `json:"authentication,omitempty"`
}

type Authentication struct {
	Type      string                      `json:"type,omitempty"`
	SecretRef corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// ContentConfigurationStatus defines the observed state of ContentConfiguration
type ContentConfigurationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions          []metav1.Condition `json:"conditions,omitempty"`
	ObservedGeneration  int64              `json:"observedGeneration,omitempty" protobuf:"varint,3,opt,name=observedGeneration"`
	NextReconcileTime   metav1.Time        `json:"nextReconcileTime,omitempty"`
	ConfigurationResult string             `json:"configurationResult,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ContentConfiguration is the Schema for the contentconfigurations API
// +kubebuilder:resource:scope=Cluster,shortName=cc
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Valid",type=string,JSONPath=`.status.conditions[?(@.type=="Valid")].status`
type ContentConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ContentConfigurationSpec   `json:"spec,omitempty"`
	Status ContentConfigurationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ContentConfigurationList contains a list of ContentConfiguration
type ContentConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ContentConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion,
			&ContentConfiguration{},
			&ContentConfigurationList{},
		)
		metav1.AddToGroupVersion(s, GroupVersion)
		return nil
	})
}

func (i *ContentConfiguration) GetConditions() []metav1.Condition { return i.Status.Conditions }

func (i *ContentConfiguration) SetConditions(conditions []metav1.Condition) {
	i.Status.Conditions = conditions
}

func (i *ContentConfiguration) GetObservedGeneration() int64      { return i.Status.ObservedGeneration }
func (i *ContentConfiguration) SetObservedGeneration(g int64)     { i.Status.ObservedGeneration = g }
func (i *ContentConfiguration) GetNextReconcileTime() metav1.Time { return i.Status.NextReconcileTime }
func (i *ContentConfiguration) SetNextReconcileTime(time metav1.Time) {
	i.Status.NextReconcileTime = time
}
