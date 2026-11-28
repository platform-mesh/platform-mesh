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

package subroutines

import (
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/errors"

	"k8s.io/apimachinery/pkg/runtime"
)

// ServiceDefaults groups the OCM images of a service.
type ServiceDefaults struct {
	ServiceName    pmcorev1alpha1.ServiceName `json:"serviceName"`
	ImageResources []ImageResource            `json:"imageResources"`
}

// ImageResource describes an OCM image
type ImageResource struct {
	Name                  string                                `json:"name,omitempty"`
	Resource              string                                `json:"resource,omitempty"`
	ResourceName          string                                `json:"resourceName,omitempty"`
	ReferencePath         []pmcorev1alpha1.ReferencePathElement `json:"referencePath,omitempty"`
	AbsoluteReferencePath []pmcorev1alpha1.ReferencePathElement `json:"absoluteReferencePath,omitempty"`
	Labels                map[string]string                     `json:"labels,omitempty"`
	Annotations           ImageResourceAnnotations              `json:"annotations,omitempty"`
}

// ImageResourceAnnotations defines the OCM image mapping annotations.
type ImageResourceAnnotations struct {
	Repo      string `json:"repo,omitempty"`
	Artifact  string `json:"artifact,omitempty"`
	For       string `json:"for,omitempty"`
	Unsuspend string `json:"unsuspend,omitempty"`
	Path      string `json:"path,omitempty"`
	ImageRef  string `json:"image-ref,omitempty"`
}

// injectDefaultImageResources fills missing imageResources for configured services.
// Service enablement is supplied separately by PlatformMesh.spec.services.
func injectDefaultImageResources(services map[string]any) error {
	for _, defaults := range DefaultImageResources {
		service, ok := services[string(defaults.ServiceName)].(map[string]any)
		if !ok || service == nil {
			continue
		}
		if _, ok := service["imageResources"]; ok {
			continue
		}
		resources := make([]any, 0, len(defaults.ImageResources))
		for _, image := range defaults.ImageResources {
			resource, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&image)
			if err != nil {
				return errors.Wrap(err, "Failed to serialize default image resource for %s", defaults.ServiceName)
			}
			resources = append(resources, resource)
		}
		service["imageResources"] = resources
	}
	return nil
}
