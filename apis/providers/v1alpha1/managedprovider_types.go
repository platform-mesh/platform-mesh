/*
Copyright 2026.

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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ManagedProviderPhasePending                 = "Pending"
	ManagedProviderPhaseWaitingForPlatformMesh  = "WaitingForPlatformMesh"
	ManagedProviderPhaseWaitingForProvider      = "WaitingForProvider"
	ManagedProviderPhaseCopyingKubeconfig       = "CopyingKubeconfig"
	ManagedProviderPhaseCopyingKubeconfigFailed = "CopyingKubeconfigFailed"
	ManagedProviderPhaseDeploying               = "Deploying"
	ManagedProviderPhaseReady                   = "Ready"
	ManagedProviderPhaseDeleting                = "Deleting"
)

// ManagedProviderSpec defines the desired state of ManagedProvider.
// ManagedProvider is a runtime-cluster resource that orchestrates the full
// provider lifecycle: workspace creation, Provider bootstrap, secret copy,
// and workload deployment.
type ManagedProviderSpec struct {
	// provider references the Provider resource.
	// If not provided, a corresponding Provider is created in root:providers:system by default.
	// If corresponding Provider doesn't exist, it is created.
	// If specified, the workspace ProviderReference.Path must exist.
	//
	// +optional
	ProviderReference *ProviderReferenceSpec `json:"provider,omitempty"`

	// providerHostOverride overrides the kcp front-proxy host written into the
	// generated kubeconfig Secret. Optional; defaults to the operator-configured
	// front-proxy URL.
	// +optional
	ProviderHostOverride string `json:"providerHostOverride,omitempty"`

	// runtimeKubeconfigSecretName is the name of the Secret that contains
	// data.kubeconfig key, with kubeconfig referencing the runtime cluster
	// where the provider components are to be deployed.
	// If not provided, the hosting platform-mesh cluster is used.
	//
	// +optional
	RuntimeKubeconfigSecretName string `json:"runtimeKubeconfigSecretName,omitempty"`

	// providerKubeconfigSecret is a Secret specification used to store
	// the provider's kubeconfig in the runtime cluster. This is the admin kubeconfig
	// that provider controllers can use to access the workspace in :root:providers:<WS name>.
	// If not provided, a default of Name:<ManagedProvider.Name>-provider-kubeconfig,
	// Key:kubeconfig is used.
	//
	// +optional
	ProviderKubeconfigSecret *LocalKubeconfigSecretSpec `json:"providerKubeconfigSecret,omitempty"`

	// platformMeshRef is a reference to the PlatformMesh object.
	// It must refer to the PlatformMesh instance this ManagedProvider
	// is associated with.
	PlatformMeshReference PlatformMeshReferenceSpec `json:"platformMeshRef"`

	// runtimeDeployments is a list of components to be deployed in the runtime cluster.
	RuntimeDeployments []ProviderComponentSpec `json:"runtimeDeployments"`

	// cleanupOnDelete removes the kcp workspace when the ManagedProvider is deleted.
	// +optional
	CleanupOnDelete bool `json:"cleanupOnDelete,omitempty"`
}

// PlatformMeshReferenceSpec is a reference to a PlatformMesh object.
type PlatformMeshReferenceSpec struct {
	// name of the PlatformMesh object.
	// +required
	Name string `json:"name"`
}

// ProviderReferenceSpec is a reference to a Provider object.
type ProviderReferenceSpec struct {
	// path is a logical cluster path where the Provider is defined.
	//
	// +required
	// +kubebuilder:validation:Pattern:="^[a-z0-9]([-a-z0-9]*[a-z0-9])?(:[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$"
	Path string `json:"path"`

	// name is the name of the referenced Provider.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kube:validation:MinLength=1
	Name string `json:"name"`
}

// LocalKubeconfigSecretSpec describes a Secret and the key with kubeconfig YAML content.
type LocalKubeconfigSecretSpec struct {
	// name is the name of the Secret.
	Name string `json:"name"`

	// key is the key in data map where the kubeconfig YAML content is stored.
	Key string `json:"key"`
}

// ProviderComponentSpec references a single component to deploy in the runtime cluster.
// Exactly one of flux or ocm must be set.
type ProviderComponentSpec struct {
	// flux deploys a Helm chart from an OCI registry or HTTP Helm repository directly
	// via Flux (OCIRepository/HelmRepository + HelmRelease). No OCM descriptor
	// resolution is performed.
	// +optional
	Flux *FluxComponentSpec `json:"flux,omitempty"`

	// ocm deploys a chart resolved from an OCM component descriptor. The operator emits
	// a delivery.ocm.software Resource that references an existing Repository and
	// Component; the ocm-controller resolves the descriptor (signatures, references,
	// relocation) and the resolved chart is then deployed via Flux.
	// +optional
	OCM *OCMComponentSpec `json:"ocm,omitempty"`
}

// FluxSourceType selects how a FluxComponentSpec chart is fetched.
const (
	// FluxSourceTypeOCI fetches a chart packaged as an OCI artifact (oci://) via a
	// Flux OCIRepository.
	FluxSourceTypeOCI = "oci"
	// FluxSourceTypeHelm fetches a chart from a classic HTTP(S) Helm repository via a
	// Flux HelmRepository.
	FluxSourceTypeHelm = "helm"
)

// FluxComponentSpec identifies a Helm chart deployed via Flux, either from an OCI
// registry or from a classic HTTP(S) Helm repository.
type FluxComponentSpec struct {
	// type selects how the chart is fetched:
	//   - "oci" (default): the chart is packaged as an OCI artifact in an OCI registry,
	//     deployed via a Flux OCIRepository.
	//   - "helm": the chart is served from a classic HTTP(S) Helm repository,
	//     deployed via a Flux HelmRepository.
	// +kubebuilder:validation:Enum=oci;helm
	// +kubebuilder:default=oci
	// +optional
	Type string `json:"type,omitempty"`

	// registry is the chart source location:
	//   - for type=oci: the OCI registry host and base path that holds the chart
	//     (e.g. ghcr.io/platform-mesh/provider-quickstart/charts).
	//   - for type=helm: the Helm repository URL (e.g. https://charts.jetstack.io).
	// +required
	Registry string `json:"registry"`

	// chart is the chart name:
	//   - for type=oci: the chart's OCI repository path under the registry
	//     (e.g. wildwest-armament-sync).
	//   - for type=helm: the chart name within the Helm repository (e.g. cert-manager).
	// +required
	Chart string `json:"chart"`

	// version is the chart version to deploy (the OCI tag for type=oci, or the chart
	// version for type=helm).
	// +required
	Version string `json:"version"`

	// values are Helm values passed to the deployed chart.
	// +optional
	Values apiextensionsv1.JSON `json:"values,omitempty"`

	// insecure allows HTTP (insecure) connection to the OCI repository.
	// Disabled by default.
	// +optional
	Insecure bool `json:"insecure,omitempty"`
}

// OCMComponentSpec deploys a chart resolved from an OCM component descriptor. The OCM
// coordinates are given inline; the operator creates the delivery.ocm.software
// Repository, Component and Resource objects, the ocm-controller resolves the
// descriptor and writes the resolved chart artifact into the Resource status, which is
// then deployed via Flux.
type OCMComponentSpec struct {
	// name is the deployment name used for the generated Repository, Component,
	// Resource, OCIRepository and HelmRelease. If empty it defaults to resourceName,
	// then the last referencePath element, then the last segment of the component name.
	// Set it explicitly when several components share the same component name (so the
	// generated objects don't collide).
	// +optional
	Name string `json:"name,omitempty"`

	// registry is the OCM/OCI registry root that holds the component
	// (e.g. ghcr.io/platform-mesh). The operator creates a delivery.ocm.software
	// Repository from it (baseUrl = host, subPath = remaining path).
	// +required
	Registry string `json:"registry"`

	// component is the fully-qualified OCM component name
	// (e.g. github.com/platform-mesh/provider-quickstart). The operator creates a
	// delivery.ocm.software Component from it.
	// +required
	Component string `json:"component"`

	// version is the OCM component version (semver).
	// +required
	Version string `json:"version"`

	// resourceName is the OCM resource name within the component to deploy.
	// Defaults to "chart".
	// +optional
	ResourceName string `json:"resourceName,omitempty"`

	// referencePath navigates the component reference graph to reach the resource.
	// Each element selects a nested component reference by name. Empty resolves a
	// resource directly on the component.
	// +optional
	ReferencePath []OCMReferencePathElement `json:"referencePath,omitempty"`

	// values are Helm values passed to the resolved chart.
	// +optional
	Values apiextensionsv1.JSON `json:"values,omitempty"`

	// insecure allows HTTP (insecure) connection to the registry.
	// Disabled by default.
	// +optional
	Insecure bool `json:"insecure,omitempty"`
}

// OCMReferencePathElement is a single element of an OCM component reference path.
type OCMReferencePathElement struct {
	// name of the component reference to follow.
	// +required
	Name string `json:"name"`
}

// ManagedProviderStatus defines the observed state of ManagedProvider.
type ManagedProviderStatus struct {
	// phase summarises the overall lifecycle state of the ManagedProvider.
	// +optional
	Phase string `json:"phase,omitempty"`

	// providerKubeconfigSecretRef points to the Secret in the runtime namespace that
	// contains the scoped kubeconfig copied from the provider kcp workspace.
	// +optional
	ProviderKubeconfigSecretRef *corev1.SecretReference `json:"providerKubeconfigSecretRef,omitempty"`

	// conditions represent the current state of the ManagedProvider resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the last generation reconciled by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string,description="Overall lifecycle phase"
// +kubebuilder:printcolumn:JSONPath=".status.conditions[?(@.type=='Ready')].status",name="Ready",type=string,description="Shows if resource is ready"

// ManagedProvider is the Schema for the managedproviders API.
// It orchestrates the full provider lifecycle from the runtime cluster side.
type ManagedProvider struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of ManagedProvider
	// +required
	Spec ManagedProviderSpec `json:"spec"`

	// status defines the observed state of ManagedProvider
	// +optional
	Status ManagedProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ManagedProviderList contains a list of ManagedProvider
type ManagedProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedProvider{}, &ManagedProviderList{})
}

func (i *ManagedProvider) GetConditions() []metav1.Condition {
	return i.Status.Conditions
}

func (i *ManagedProvider) SetConditions(conditions []metav1.Condition) {
	i.Status.Conditions = conditions
}
