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
