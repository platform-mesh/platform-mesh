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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Icon struct {
	Light Image `json:"light"`
	Dark  Image `json:"dark"`
}

type URL struct {
	URL string `json:"url,omitempty"`
}

type Image struct {
	URL  string `json:"url,omitempty"`
	Data string `json:"data,omitempty"`
}

type Link struct {
	DisplayName string `json:"displayName,omitempty"`
	URL         string `json:"url,omitempty"`
}

type Contact struct {
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email,omitempty"`
	Role        []string `json:"role,omitempty"`
}

// DetailViewExtension describes a provider-owned micro frontend that enriches
// the provider details view. The extension is rendered in list order.
type DetailViewExtension struct {
	// URL is the absolute URL of the micro frontend.
	URL string `json:"url"`
}

// Label is a colored chip rendered in the provider card header.
type Label struct {
	// Title is the chip text.
	Title string `json:"title"`
	// Color is the Fundamental info-label color category (1-10).
	// +kubebuilder:validation:Enum="1";"2";"3";"4";"5";"6";"7";"8";"9";"10"
	Color string `json:"color"`
	// Glyph is an optional SAP-icons glyph name shown on the chip.
	Glyph string `json:"glyph,omitempty"`
}

// ServiceLevel describes the support coverage offered by a provider, rendered
// in the "Service Level" row of the provider details view.
// +kubebuilder:validation:Enum=veryHigh24x7;high24x5;mediumOne16x5;mediumTwo12x5;low8x5
type ServiceLevel string

const (
	ServiceLevelVeryHigh  ServiceLevel = "veryHigh24x7"
	ServiceLevelHigh      ServiceLevel = "high24x5"
	ServiceLevelMediumOne ServiceLevel = "mediumOne16x5"
	ServiceLevelMediumTwo ServiceLevel = "mediumTwo12x5"
	ServiceLevelLow       ServiceLevel = "low8x5"
)

// Verification describes the trust level of a provider, rendered as a badge in
// the marketplace UI.
type Verification struct {
	// Label is the text shown on the verification badge.
	Label string `json:"label"`
	// Status is the badge color, mapped to a Fundamental ObjectStatus.
	// +kubebuilder:validation:Enum=positive;critical;negative;informative;neutral
	Status string `json:"status,omitempty"`
	// Icon is an optional SAP-icons glyph name; the UI defaults to a verified
	// glyph when it is absent.
	Icon string `json:"icon,omitempty"`
	// Hint is an optional tooltip shown on the badge.
	Hint string `json:"hint,omitempty"`
}

// ProviderMetadataSpec defines the desired state of ProviderMetadata.
type ProviderMetadataSpec struct {
	Tags []string `json:"tags,omitempty"`

	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
	Category    string `json:"category,omitempty"`

	// Additional information that should be stored with the provider metadata.
	Data          *apiextensionsv1.JSON `json:"data,omitempty"`
	Contacts      []Contact             `json:"contacts,omitempty"`
	Documentation []Link                `json:"documentation,omitempty"`
	Icon          *Icon                 `json:"icon,omitempty"`

	Links                    []Link                `json:"links,omitempty"`
	MainLink                 *Link                 `json:"mainLink,omitempty"`
	PreferredSupportChannels []Link                `json:"preferredSupportChannels,omitempty"`
	HelpCenterData           []Link                `json:"helpCenterData,omitempty"`
	DetailViewExtensions     []DetailViewExtension `json:"detailViewExtensions,omitempty"`
	Labels                   []Label               `json:"labels,omitempty"`
	ServiceLevel             ServiceLevel          `json:"serviceLevel,omitempty"`
	Verification             *Verification         `json:"verification,omitempty"`
}

// ProviderMetadataStatus defines the observed state of ProviderMetadata.
type ProviderMetadataStatus struct{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=providermetadatas

// ProviderMetadata is the Schema for the providermetadata API.
// +kubebuilder:resource:scope=Cluster
type ProviderMetadata struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderMetadataSpec   `json:"spec,omitempty"`
	Status ProviderMetadataStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderMetadataList contains a list of ProviderMetadata.
type ProviderMetadataList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderMetadata `json:"items"`
}
