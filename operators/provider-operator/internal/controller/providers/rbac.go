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

package providers

// What the ManagedProvider subroutines touch on the runtime cluster. The markers are kept
// out of managedprovider_controller.go so that file stays identical to platform-mesh-operator's.
//
// Secrets and the Flux and OCM objects are read uncached, so they need no list or watch.
//
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;create;update;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=create
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=ocirepositories;helmrepositories,verbs=get;create;update;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;create;update;delete
// +kubebuilder:rbac:groups=delivery.ocm.software,resources=repositories;components;resources,verbs=get;create;update;delete
