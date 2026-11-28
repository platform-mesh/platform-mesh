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
)

// applyServiceEnablement applies the instance's complete service selection.
// An absent selection preserves legacy profile/spec enablement.
func applyServiceEnablement(services map[string]any, selection []pmcorev1alpha1.ServiceConfiguration) (map[string]any, error) {
	if selection == nil {
		return services, nil
	}

	supported := make(map[pmcorev1alpha1.ServiceName]struct{}, len(DefaultImageResources))
	for _, defaults := range DefaultImageResources {
		supported[defaults.ServiceName] = struct{}{}
	}
	// Validate the entire selection before changing any service settings.
	seen := make(map[pmcorev1alpha1.ServiceName]struct{}, len(selection))
	for _, setting := range selection {
		if _, ok := supported[setting.Name]; !ok {
			return nil, errors.New("service %q in spec.services is not supported", setting.Name)
		}
		if _, ok := seen[setting.Name]; ok {
			return nil, errors.New("spec.services contains duplicate service %q", setting.Name)
		}
		seen[setting.Name] = struct{}{}
		if setting.Enabled {
			service, ok := services[string(setting.Name)].(map[string]any)
			if !ok || service == nil {
				return nil, errors.New("service %q is enabled in spec.services but has no configuration in the profile or spec.values", setting.Name)
			}
		}
	}

	// Keep deployment settings intact while disabling unlisted and custom services.
	for _, raw := range services {
		if service, ok := raw.(map[string]any); ok && service != nil {
			service["enabled"] = false
		}
	}
	for _, setting := range selection {
		if service, ok := services[string(setting.Name)].(map[string]any); ok && service != nil {
			service["enabled"] = setting.Enabled
		}
	}
	return services, nil
}
