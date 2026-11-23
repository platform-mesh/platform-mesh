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
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/pkg/merge"

	"k8s.io/apimachinery/pkg/runtime"
)

type helmValues struct {
	Exposure helmExposureValues `json:"exposure"`
	KCP      helmKCPValues      `json:"kcp"`
}

type helmExposureValues struct {
	BaseDomain string `json:"baseDomain"`
	Protocol   string `json:"protocol"`
	Port       int    `json:"port"`
	// NormalizedPort is the URL suffix; Port stays numeric for Gateway listeners and portal configuration.
	NormalizedPort   string `json:"normalizedPort"`
	GatewayClassName string `json:"gatewayClassName"`
}

type helmKCPValues struct {
	InternalFrontProxyURL string `json:"internalFrontProxyUrl"`
}

// Only the fields used for injection are decoded; other service settings stay in the original map.
type helmService struct {
	External bool           `json:"external,omitempty"`
	Values   map[string]any `json:"values"`
}

// buildHelmValues resolves operator-owned chart inputs without consulting a profile.
func buildHelmValues(inst *pmcorev1alpha1.PlatformMesh, cfg *config.OperatorConfig) helmValues {
	exposure := getExposureParams(inst, cfg.Exposure)
	normalizedPort := fmt.Sprintf(":%d", exposure.port)
	// Omit default ports to preserve advertised URLs and OIDC issuer strings.
	if (exposure.protocol == "http" && exposure.port == 80) || (exposure.protocol == "https" && exposure.port == 443) {
		normalizedPort = ""
	}
	gatewayClassName := cfg.Exposure.GatewayClassName
	if gatewayClassName == "" {
		gatewayClassName = "traefik"
	}
	return helmValues{
		Exposure: helmExposureValues{
			BaseDomain:       exposure.baseDomain,
			Protocol:         exposure.protocol,
			Port:             exposure.port,
			NormalizedPort:   normalizedPort,
			GatewayClassName: gatewayClassName,
		},
		KCP: helmKCPValues{
			InternalFrontProxyURL: exposure.internalFrontProxyURL(cfg.KCP),
		},
	}
}

func injectHelmValues(services map[string]any, values helmValues, log *logger.Logger) (map[string]any, error) {
	var configs map[string]*helmService
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(services, &configs); err != nil {
		return nil, errors.Wrap(err, "Failed to decode Helm service configuration")
	}
	overrides, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&values)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to serialize operator Helm values")
	}
	for name, service := range configs {
		if service == nil || service.External {
			delete(configs, name)
			continue
		}
		service.Values, err = merge.MergeMaps(service.Values, overrides, log)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to inject Helm values into service %s", name)
		}
	}
	resolved, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&configs)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to serialize Helm service configuration")
	}
	// Merge the typed fields back to preserve arbitrary service metadata and external components.
	return merge.MergeMaps(services, resolved, log)
}
