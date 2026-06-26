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

// Package restore provides the etcd restore subroutine for PlatformRestore reconciliation.
package restore

// +kubebuilder:rbac:groups=druid.gardener.cloud,resources=etcds,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=backup.platform-mesh.io,resources=platformbackups,verbs=get;list;watch
// +kubebuilder:rbac:groups=backup.platform-mesh.io,resources=platformrestores,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=backup.platform-mesh.io,resources=platformrestores/status,verbs=get;update;patch
