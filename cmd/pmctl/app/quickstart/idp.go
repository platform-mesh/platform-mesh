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
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// The identity provider of the installation. Dex with static users is the
// small one: no Keycloak, no database for it, none of the services that
// manage Keycloak realms and users. Keycloak is what production runs.
const (
	// IDPDex and IDPKeycloak are the values of Options.IDP.
	IDPDex      = "dex"
	IDPKeycloak = "keycloak"

	// disableKeycloakToggle is the PlatformMesh feature toggle that leaves
	// Keycloak to someone else; spec.authentication needs it.
	disableKeycloakToggle = "feature-disable-keycloak"
	// platformMeshValuesKey is the value of the operator chart that adds to
	// the PlatformMesh resource. Charts without it cannot install Dex.
	platformMeshValuesKey = "platformMesh"

	// The OIDC client of Dex that the portal logs users in with and kcp
	// accepts tokens of. The secret is no secret: local installations only.
	dexClientID     = "platform-mesh"
	dexClientSecret = "local-dev-platform-mesh-secret" //nolint:gosec // local installations only
	// Dex adds e-mail and groups to a token only for their scopes;
	// offline_access gives a refresh token.
	dexScope = "openid email profile groups offline_access"

	// dexKubectlClientID is the client of Dex kubeconfigs log in with. It is
	// public: no secret, and any http://localhost redirect, which is where
	// "kubectl oidc-login" listens.
	dexKubectlClientID = "kubectl"

	// DexPassword is the password of every user in DexUsers.
	DexPassword = "password"
	// dexPasswordHash is the bcrypt hash of DexPassword.
	dexPasswordHash = "$2a$10$iwYOwbUyunwL.Ahtr9zBq.ruc.rjYSDPwBnX.CDAsMlUA1alMoeqW" //nolint:gosec // local installations only
)

// keycloakServices are the services of the profile that only exist for
// Keycloak. The operator refuses the disableKeycloakToggle while they are on.
// dexKubectlScopes are the scopes kubeconfigs request besides openid. kcp
// accepts tokens for dexClientID only; Dex adds that audience to a token of
// dexKubectlClientID for the last scope, as dexClientID trusts it.
var dexKubectlScopes = []string{"email", "groups", "audience:server:client_id:" + dexClientID}

var keycloakServices = []string{"pm-keycloak-operator", "iam-service", "iam-ui"}

// dexUsers are the static users of Dex, the same as in the Tilt environment
// (contrib/tilt/tiltfiles/settings.Tiltfile).
var dexUsers = []dexUserValues{
	{Email: "admin@portal.localhost", Username: "admin", UserID: "69b09731-2f2e-454e-b08c-2186ec99c75e", Hash: dexPasswordHash, Groups: []string{"platform-mesh:admins"}},
	{Email: "dev@portal.localhost", Username: "dev", UserID: "186d270a-8446-4c96-bbb8-6aa95ffdb6e7", Hash: dexPasswordHash, Groups: []string{"platform-mesh:developers"}},
	{Email: "viewer@portal.localhost", Username: "viewer", UserID: "9858cb7f-f74f-4932-818f-56d69459dac1", Hash: dexPasswordHash, Groups: []string{"platform-mesh:viewers"}},
}

// DexUsers returns the e-mail addresses of the users of an installation with
// Dex. Their password is DexPassword.
func DexUsers() []string {
	emails := make([]string, 0, len(dexUsers))
	for _, user := range dexUsers {
		emails = append(emails, user.Email)
	}
	return emails
}

// DexIssuer is the address of Dex, and the issuer of its tokens.
func (o *Options) DexIssuer() string {
	return "https://" + net.JoinHostPort(o.BaseDomain, strconv.Itoa(gatewayPort)) + "/dex"
}

// resolveIDP settles o.IDP for installing src. An installation keeps the
// identity provider it has; a new one gets Dex, unless the charts of src are
// from before the operator chart could set it up.
func (o *Options) resolveIDP(helm *helmClient, src *source) error {
	requested := o.IDP

	last, err := helm.lastRelease(operatorChart)
	if err != nil {
		return err
	}
	if last != nil {
		installed := IDPKeycloak
		toggles, _, _ := unstructured.NestedStringSlice(last.Config, platformMeshValuesKey, "extraFeatureToggles")
		if slices.Contains(toggles, disableKeycloakToggle) {
			installed = IDPDex
		}
		if requested != "" && requested != installed {
			// The users and organizations of one are unknown to the other.
			return fmt.Errorf("the installation uses %s and cannot be switched to %s: remove it first with \"pmctl quickstart delete --name %s\"", installed, requested, o.Name)
		}
		o.IDP = installed
	}

	if o.IDP != IDPKeycloak {
		defaults, err := helm.defaultValues(src.operator, src.chartVersion)
		if err != nil {
			return err
		}
		_, supported := defaults[platformMeshValuesKey]
		switch {
		case supported:
			o.IDP = IDPDex
		case o.IDP == IDPDex:
			return fmt.Errorf("the charts of Platform Mesh %s cannot install Dex yet: use --idp=%s, or a newer version", src.version, IDPKeycloak)
		default:
			_, _ = fmt.Fprintf(o.ErrOut, "The charts of Platform Mesh %s cannot install Dex yet: using Keycloak as identity provider.\n", src.version)
			o.IDP = IDPKeycloak
		}
	}

	if o.IDP == IDPDex {
		if len(o.keycloakFlags) > 0 {
			_, _ = fmt.Fprintf(o.ErrOut, "Ignoring --%s: only used with --idp=%s.\n", strings.Join(o.keycloakFlags, ", --"), IDPKeycloak)
		}
		// Dex has static users: nobody signs up, is invited, or has an
		// e-mail to verify.
		o.RegistrationAllowed = false
		o.AllowUnverifiedEmails = false
		o.SetDefaultPassword = false
	}
	return nil
}

// dexValues are the additions to the PlatformMesh resource that replace
// Keycloak with Dex: what "tilt up -- --with=dex" does through chart values,
// said through the operator.
//
// manifest is a rendering of the operator chart. Its profile has the
// environment of the portal, which the additions extend: the operator
// replaces lists, it does not merge them.
func (o *Options) dexValues(manifest string) (*platformMeshValues, error) {
	portalEnv, err := portalEnvOf(manifest)
	if err != nil {
		return nil, err
	}
	portalEnv = append(portalEnv,
		envVarValues{"name": "OIDC_STATIC_CLIENT_ID", "value": dexClientID},
		envVarValues{"name": "OIDC_STATIC_CLIENT_SECRET", "value": dexClientSecret},
		envVarValues{"name": "OIDC_SCOPE", "value": dexScope},
		// Dex has no wildcard redirect URIs: only the callback on the base
		// domain is registered, not the ones of the organizations.
		envVarValues{"name": "OIDC_CALLBACK_ON_BASE_DOMAIN", "value": "true"},
	)

	services := map[string]serviceValues{
		"infra": {Values: infraValues{
			Keycloak: infraKeycloakValues{Operator: enabledValues{Enabled: false}},
			Dex: dexValues{
				Enabled:    true,
				Issuer:     o.DexIssuer(),
				GatewayAPI: dexGatewayAPIValues{Hostnames: []string{o.BaseDomain}},
				// The password grant issues tokens without a browser.
				PasswordConnector: "local",
				// Dex v2.46.1 deadlocks its memory storage when it rotates
				// a refresh token; without rotation and inside the reuse
				// interval it returns before that.
				Expiry:          dexExpiryValues{RefreshTokens: dexRefreshTokenValues{DisableRotation: true, ReuseInterval: "8760h"}},
				StaticPasswords: dexUsers,
				StaticClient: dexClientValues{
					ID:     dexClientID,
					Secret: dexClientSecret,
					Name:   "Platform Mesh",
					// Dex compares redirect URIs exactly; this is the
					// callback of the portal.
					RedirectURIs: []string{o.URL() + "callback?storageType=none"},
					TrustedPeers: []string{dexKubectlClientID},
				},
				ExtraStaticClients: []dexExtraClientValues{{ID: dexKubectlClientID, Name: "kubectl", Public: true}},
			},
		}},
		"portal": {Values: portalValues{ExtraEnvVars: portalEnv}},
	}
	for _, name := range keycloakServices {
		services[name] = serviceValues{Enabled: new(false)}
	}

	return &platformMeshValues{
		ExtraFeatureToggles: []string{disableKeycloakToggle},
		// kcp and the portal: the operator derives their values from this.
		Authentication: &authenticationValues{OIDC: oidcValues{
			IssuerURL:       o.DexIssuer(),
			ClientID:        dexClientID,
			KubectlClientID: dexKubectlClientID,
			KubectlScopes:   dexKubectlScopes,
		}},
		Values: &platformMeshSpecValues{Services: services},
	}, nil
}

// portalEnvOf returns the environment the profile in manifest, a rendering of
// the operator chart, gives the portal.
func portalEnvOf(manifest string) ([]envVarValues, error) {
	cm, err := profileOf(manifest)
	if err != nil {
		return nil, err
	}
	// The profile is a template of the operator, and YAML before it is
	// rendered as well: the templates sit inside strings.
	var profile struct {
		Components struct {
			Services map[string]struct {
				Values struct {
					ExtraEnvVars []envVarValues `json:"extraEnvVars"`
				} `json:"values"`
			} `json:"services"`
		} `json:"components"`
	}
	if err := yaml.Unmarshal([]byte(cm.Data[profileKey]), &profile); err != nil {
		return nil, fmt.Errorf("reading the profile of the %s chart: %w", operatorChart, err)
	}
	portal, ok := profile.Components.Services["portal"]
	if !ok {
		return nil, fmt.Errorf("the profile of the %s chart has no portal service", operatorChart)
	}
	return portal.Values.ExtraEnvVars, nil
}

// withIDP adds what o.IDP needs to the operator chart's values.
func (o *Options) withIDP(ctx context.Context, helm *helmClient, src *source, values operatorValues) (operatorValues, error) {
	if o.IDP != IDPDex {
		return values, nil
	}
	manifest, err := helm.render(ctx, operatorChart, src.operator, src.chartVersion, values)
	if err != nil {
		return values, err
	}
	values.PlatformMesh, err = o.dexValues(manifest)
	return values, err
}
