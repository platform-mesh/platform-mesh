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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// OwnedByAnnotation records the platform-mesh component that bootstrapped
	// (created) a resource. Components set it on every object they seed so the
	// provenance of shared, cluster-wide bootstrap objects (WorkspaceTypes,
	// APIExports, workspaces, ...) is discoverable and attributable to an owner.
	//
	// The value is the owning component's name, e.g. "provider-operator".
	OwnedByAnnotation = GroupName + "/owned-by"
)

// SetOwnedBy stamps component as the bootstrapper/owner of obj via
// OwnedByAnnotation. It is shared so every component that seeds resources marks
// ownership the same way. It only sets the annotation when it is not already
// present, so it never re-attributes a resource another component already owns.
func SetOwnedBy(obj metav1.Object, component string) {
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	if _, ok := ann[OwnedByAnnotation]; ok {
		return
	}
	ann[OwnedByAnnotation] = component
	obj.SetAnnotations(ann)
}
