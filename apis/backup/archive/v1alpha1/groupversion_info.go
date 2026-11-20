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

// Package v1alpha1 contains the types in a backup archive of the archive.backup.platform-mesh.io
// group. They are files in the archive, not objects in a cluster, so there are no CRDs.
// +kubebuilder:object:generate=true
// +kubebuilder:skip
// +groupName=archive.backup.platform-mesh.io
package v1alpha1

import "k8s.io/apimachinery/pkg/runtime/schema"

const (
	GroupName    = "archive.backup.platform-mesh.io"
	GroupVersion = "v1alpha1"
)

// SchemeGroupVersion is the apiVersion of the files in an archive.
var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: GroupVersion}
