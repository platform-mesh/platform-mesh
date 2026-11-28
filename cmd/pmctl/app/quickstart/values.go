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

package quickstart

import (
	"encoding/json"
	"fmt"
)

// The values quickstart sets on the two charts, as types: only what it sets,
// not the charts' whole values.

// TODO(mjudeikis): Add public facing API for these chart values. Fow now we
// only define them internally.

// prerequisitesValues are values of the platform-mesh-prerequisites chart.
type prerequisitesValues struct {
	KeycloakOperator keycloakOperatorValues `json:"keycloak-operator"`
	Flux2            flux2Values            `json:"flux2"`
	// Traefik is unset in the first pass, which leaves it disabled.
	Traefik *traefikValues `json:"traefik,omitempty"`
}

type keycloakOperatorValues struct {
	Enabled         bool   `json:"enabled"`
	WatchNamespaces string `json:"watchNamespaces"`
}

type flux2Values struct {
	ImageAutomationController fluxControllerValues `json:"imageAutomationController"`
	ImageReflectionController fluxControllerValues `json:"imageReflectionController"`
	HelmController            helmControllerValues `json:"helmController"`
}

type helmControllerValues struct {
	Container fluxContainerValues `json:"container"`
}

type fluxContainerValues struct {
	AdditionalArgs []string `json:"additionalArgs"`
}

type fluxControllerValues struct {
	Create bool `json:"create"`
}

type traefikValues struct {
	Enabled bool                 `json:"enabled"`
	Service traefikServiceValues `json:"service"`
	Ports   traefikPortsValues   `json:"ports"`
}

type traefikServiceValues struct {
	Type string                   `json:"type"`
	Spec traefikServiceSpecValues `json:"spec"`
}

type traefikServiceSpecValues struct {
	Type string `json:"type"`
}

type traefikPortsValues struct {
	WebSecure traefikPortValues `json:"websecure"`
}

type traefikPortValues struct {
	ExposedPort int `json:"exposedPort"`
	NodePort    int `json:"nodePort"`
}

// operatorValues are values of the platform-mesh-operator chart.
type operatorValues struct {
	OCM          ocmValues          `json:"ocm"`
	Gateway      gatewayValues      `json:"gateway"`
	KCP          kcpValues          `json:"kcp"`
	Certificates certificatesValues `json:"certificates"`
	IDP          idpValues          `json:"idp"`
	// Image overrides the operator image; unset uses the chart's appVersion.
	Image *imageValues `json:"image,omitempty"`
	// PlatformMesh adds to the PlatformMesh resource; unset for Keycloak,
	// which is what the chart installs by itself.
	PlatformMesh *platformMeshValues `json:"platformMesh,omitempty"`
	// Services selects components through charts that render spec.services.
	Services map[string]enabledValues `json:"services,omitempty"`
}

// platformMeshValues are the chart's additions to the PlatformMesh resource.
type platformMeshValues struct {
	ExtraFeatureToggles []string `json:"extraFeatureToggles,omitempty"`
	// Authentication is spec.authentication.
	Authentication *authenticationValues `json:"authentication,omitempty"`
	// Values is spec.values, merged over the services of the profile.
	Values *platformMeshSpecValues `json:"values,omitempty"`
}

type authenticationValues struct {
	OIDC oidcValues `json:"oidc"`
}

type oidcValues struct {
	IssuerURL string `json:"issuerURL"`
	ClientID  string `json:"clientID"`
	// KubectlClientID and KubectlScopes are what kubeconfigs log in with.
	KubectlClientID string   `json:"kubectlClientID,omitempty"`
	KubectlScopes   []string `json:"kubectlScopes,omitempty"`
}

type platformMeshSpecValues struct {
	Services map[string]serviceValues `json:"services"`
}

// serviceValues are the settings of one service of the profile.
type serviceValues struct {
	// Enabled unset keeps the profile's.
	Enabled *bool `json:"enabled,omitempty"`
	// Values are values of the chart of the service: infraValues,
	// portalValues.
	Values any `json:"values,omitempty"`
	// DependsOn are the services this one is installed after; unset keeps
	// the profile's.
	DependsOn []dependencyValues `json:"dependsOn,omitempty"`
}

// dependencyValues names a service another one depends on.
type dependencyValues struct {
	Name string `json:"name"`
}

type enabledValues struct {
	Enabled bool `json:"enabled"`
}

// infraValues are values of the infra chart.
type infraValues struct {
	Keycloak infraKeycloakValues `json:"keycloak"`
	Dex      dexValues           `json:"dex"`
}

type infraKeycloakValues struct {
	Operator enabledValues `json:"operator"`
}

type dexValues struct {
	Enabled           bool                `json:"enabled"`
	Issuer            string              `json:"issuer"`
	GatewayAPI        dexGatewayAPIValues `json:"gatewayApi"`
	PasswordConnector string              `json:"passwordConnector"`
	Expiry            dexExpiryValues     `json:"expiry"`
	StaticPasswords   []dexUserValues     `json:"staticPasswords"`
	StaticClient      dexClientValues     `json:"staticClient"`
	// ExtraStaticClients are the clients besides StaticClient.
	ExtraStaticClients []dexExtraClientValues `json:"extraStaticClients,omitempty"`
}

type dexGatewayAPIValues struct {
	Hostnames []string `json:"hostnames"`
}

type dexExpiryValues struct {
	RefreshTokens dexRefreshTokenValues `json:"refreshTokens"`
}

type dexRefreshTokenValues struct {
	DisableRotation bool   `json:"disableRotation"`
	ReuseInterval   string `json:"reuseInterval"`
}

type dexUserValues struct {
	Email    string   `json:"email"`
	Username string   `json:"username"`
	UserID   string   `json:"userID"`
	Hash     string   `json:"hash"`
	Groups   []string `json:"groups"`
}

type dexClientValues struct {
	ID           string   `json:"id"`
	Secret       string   `json:"secret"`
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirectURIs"`
	// TrustedPeers are the clients that may request tokens for this one.
	TrustedPeers []string `json:"trustedPeers,omitempty"`
}

// dexExtraClientValues is a client of Dex as Dex configures it.
type dexExtraClientValues struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Public bool   `json:"public"`
}

// portalValues are values of the portal chart.
type portalValues struct {
	ExtraEnvVars []envVarValues `json:"extraEnvVars"`
}

// envVarValues is an environment variable as the profile has it, whichever
// fields it uses.
type envVarValues map[string]any

type imageValues struct {
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
}

type ocmValues struct {
	Enabled    bool   `json:"enabled"`
	Profile    string `json:"profile"`
	Version    string `json:"version"`
	Repository string `json:"repository"`
	// SignatureName is the OCM signature the component is verified with;
	// empty keeps the chart's.
	SignatureName string `json:"signatureName,omitempty"`
	// SigningIdentity is the identity that made the keyless signature, empty keeps the release tag of Version.
	SigningIdentity string `json:"signingIdentity,omitempty"`
	// InstallSigningCertificate set to false turns off the chart's own
	// signing certificate Secret; unset keeps the chart's default.
	InstallSigningCertificate *bool `json:"installSigningCertificate,omitempty"`
}

type gatewayValues struct {
	BaseDomain string `json:"baseDomain"`
	Port       int    `json:"port"`
	ClusterIP  string `json:"clusterIP"`
}

type kcpValues struct {
	FrontProxyPort int `json:"frontProxyPort"`
}

type certificatesValues struct {
	SelfSigned selfSignedValues `json:"selfSigned"`
}

type selfSignedValues struct {
	Enabled bool `json:"enabled"`
}

type idpValues struct {
	RegistrationAllowed   bool `json:"registrationAllowed"`
	AllowUnverifiedEmails bool `json:"allowUnverifiedEmails"`
	SetDefaultPassword    bool `json:"setDefaultPassword"`
}

// helmValues converts typed values to the map helm takes.
func helmValues(values any) (map[string]any, error) {
	data, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encoding chart values: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decoding chart values: %w", err)
	}
	return out, nil
}

// withServiceValues merges o.ServiceValues over what values sets on the
// services of the profile, and sets o.ServiceDependsOn on them.
func (o *Options) withServiceValues(values operatorValues) (operatorValues, error) {
	if len(o.ServiceValues) == 0 && len(o.ServiceDependsOn) == 0 {
		return values, nil
	}
	if values.PlatformMesh == nil {
		values.PlatformMesh = &platformMeshValues{}
	}
	if values.PlatformMesh.Values == nil {
		values.PlatformMesh.Values = &platformMeshSpecValues{}
	}
	services := values.PlatformMesh.Values.Services
	if services == nil {
		services = map[string]serviceValues{}
		values.PlatformMesh.Values.Services = services
	}
	for name, extra := range o.ServiceValues {
		service := services[name]
		set, err := helmValues(service.Values)
		if err != nil {
			return values, err
		}
		service.Values = mergeValues(set, extra)
		services[name] = service
	}
	for name, dependencies := range o.ServiceDependsOn {
		service := services[name]
		service.DependsOn = nil
		for _, dependency := range dependencies {
			service.DependsOn = append(service.DependsOn, dependencyValues{Name: dependency})
		}
		services[name] = service
	}
	return values, nil
}

// mergeValues merges src over dst, map by map, and returns dst.
func mergeValues(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for key, value := range src {
		if sub, ok := value.(map[string]any); ok {
			if current, ok := dst[key].(map[string]any); ok {
				dst[key] = mergeValues(current, sub)
				continue
			}
		}
		dst[key] = value
	}
	return dst
}
