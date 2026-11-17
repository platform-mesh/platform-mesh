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

// Package identityprovider implements the default provider for the neutral identity KRM API.
package identityprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/identity"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/clientreg"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/clientreg/keycloak"
)

const ownerAttribute = "platform-mesh-identity-owner"

var ErrRemoteNotFound = errors.New("provider resource not found")

type TenantObservation struct {
	Issuer, DiscoveryURL, RegistrationEndpoint string
}

// Backend is private implementation plumbing, not the replacement-provider API.
// Replacement providers implement the shared KRM API independently.
type Backend interface {
	EnsureTenant(context.Context, string, string, *bool, bool) (TenantObservation, error)
	DeleteTenant(context.Context, string, string) error
	Register(context.Context, string, clientreg.ClientMetadata) (clientreg.ClientInformation, error)
	Read(context.Context, string, clientreg.ClientInformation) (clientreg.ClientInformation, error)
	Update(context.Context, string, clientreg.ClientInformation, clientreg.ClientMetadata) (clientreg.ClientInformation, error)
	DeleteClient(context.Context, string, clientreg.ClientInformation) error
	ExternalClientID(context.Context, string, string) (string, error)
	RecoverManagementToken(context.Context, string, string, string) (string, error)
	MatchesMetadata(context.Context, string, clientreg.ClientInformation, clientreg.ClientMetadata) (bool, error)
}

type NativeConfig struct {
	AdminURL, PublicURL string
	RegistrationAllowed bool
	AccessTokenLifespan int
	SMTP                map[string]string
}

type NativeBackend struct {
	config       NativeConfig
	admin, plain *http.Client
}

func NewNativeBackend(config NativeConfig, admin, plain *http.Client) (*NativeBackend, error) {
	for _, raw := range []string{config.AdminURL, config.PublicURL} {
		if err := (identity.Principal{Issuer: raw, Subject: "provider-validation"}).Validate(); err != nil {
			return nil, fmt.Errorf("invalid keycloak provider URL: %w", err)
		}
		if strings.HasSuffix(raw, "/") {
			return nil, errors.New("keycloak provider base URLs must not end with a slash")
		}
	}
	if admin == nil || plain == nil || admin.Timeout <= 0 || plain.Timeout <= 0 {
		return nil, errors.New("bounded admin and registration HTTP clients are required")
	}
	rejectRedirect := func(_ *http.Request, _ []*http.Request) error { return errors.New("provider redirects are forbidden") }
	authenticated, registration := *admin, *plain
	authenticated.CheckRedirect, registration.CheckRedirect = rejectRedirect, rejectRedirect
	return &NativeBackend{config: config, admin: &authenticated, plain: &registration}, nil
}

func validRealm(realm string) bool {
	return realm != "" && realm != "master" && realm != "." && realm != ".." && !strings.ContainsAny(realm, "/\\?#")
}

func (b *NativeBackend) realmURL(realm string) string {
	return b.config.AdminURL + "/admin/realms/" + url.PathEscape(realm)
}

func (b *NativeBackend) ownedRealm(ctx context.Context, realm, owner string) (map[string]any, error) {
	if !validRealm(realm) || owner == "" {
		return nil, errors.New("valid nonreserved realm and immutable owner are required")
	}
	var current map[string]any
	if err := b.request(ctx, b.admin, http.MethodGet, b.realmURL(realm), nil, &current); err != nil {
		return nil, err
	}
	attributes, ok := current["attributes"].(map[string]any)
	if !ok || attributes[ownerAttribute] != owner {
		return nil, errors.New("keycloak realm is not owned by this identity tenant; explicit migration is required")
	}
	return current, nil
}

func (b *NativeBackend) EnsureTenant(ctx context.Context, realm, owner string, registration *bool, mustExist bool) (TenantObservation, error) {
	if !validRealm(realm) || owner == "" {
		return TenantObservation{}, errors.New("valid nonreserved realm and immutable owner are required")
	}
	current, err := b.ownedRealm(ctx, realm, owner)
	missing := errors.Is(err, ErrRemoteNotFound)
	if err != nil && !missing {
		return TenantObservation{}, err
	}
	if missing && mustExist {
		return TenantObservation{}, errors.New("previously managed realm is missing; refusing to replace its identity")
	}
	allowed := b.config.RegistrationAllowed
	if registration != nil {
		allowed = *registration
	}
	attributes := map[string]any{ownerAttribute: owner}
	if current != nil {
		attributes = current["attributes"].(map[string]any)
	}
	body := map[string]any{
		"realm": realm, "enabled": true, "attributes": attributes,
		"loginWithEmailAllowed": true, "registrationEmailAsUsername": true,
		"registrationAllowed": allowed, "accessTokenLifespan": b.config.AccessTokenLifespan,
		"smtpServer": b.config.SMTP,
	}
	if missing {
		// Owner marking and creation are atomic at the provider. A conflicting
		// create is retried through ownedRealm; it never becomes implicit adoption.
		err = b.request(ctx, b.admin, http.MethodPost, b.config.AdminURL+"/admin/realms", body, nil)
	} else {
		err = b.request(ctx, b.admin, http.MethodPut, b.realmURL(realm), body, nil)
	}
	if err != nil {
		return TenantObservation{}, err
	}
	var discovery struct {
		Issuer               string   `json:"issuer"`
		RegistrationEndpoint string   `json:"registration_endpoint"`
		SubjectTypes         []string `json:"subject_types_supported"`
	}
	if err := b.request(ctx, b.plain, http.MethodGet, b.config.AdminURL+"/realms/"+url.PathEscape(realm)+"/.well-known/openid-configuration", nil, &discovery); err != nil {
		return TenantObservation{}, err
	}
	expectedIssuer := b.config.PublicURL + "/realms/" + url.PathEscape(realm)
	if discovery.Issuer != expectedIssuer {
		return TenantObservation{}, errors.New("discovered issuer does not match the administrator-configured public issuer")
	}
	public := false
	for _, kind := range discovery.SubjectTypes {
		public = public || kind == "public"
	}
	if !public {
		return TenantObservation{}, errors.New("keycloak provider must support public subject identifiers")
	}
	endpoint := b.config.PublicURL + "/realms/" + url.PathEscape(realm) + "/clients-registrations/openid-connect"
	// Keycloak does not advertise registration_endpoint on every supported version.
	if discovery.RegistrationEndpoint != "" && discovery.RegistrationEndpoint != endpoint {
		return TenantObservation{}, errors.New("unexpected keycloak registration endpoint")
	}
	return TenantObservation{
		Issuer: discovery.Issuer, DiscoveryURL: expectedIssuer + "/.well-known/openid-configuration",
		RegistrationEndpoint: endpoint,
	}, nil
}

func (b *NativeBackend) DeleteTenant(ctx context.Context, realm, owner string) error {
	if _, err := b.ownedRealm(ctx, realm, owner); err != nil {
		if errors.Is(err, ErrRemoteNotFound) {
			return nil
		}
		return err
	}
	err := b.request(ctx, b.admin, http.MethodDelete, b.realmURL(realm), nil, nil)
	if errors.Is(err, ErrRemoteNotFound) {
		return nil
	}
	return err
}

func (b *NativeBackend) registrationClient(realm string) clientreg.Client {
	return clientreg.NewClient(
		clientreg.WithHTTPClient(b.plain),
		clientreg.WithTokenProvider(keycloak.NewAdminClient(b.admin, b.config.AdminURL, realm)),
	)
}

func (b *NativeBackend) managementURI(realm, public string) (string, error) {
	prefix := b.config.PublicURL + "/realms/" + url.PathEscape(realm) + "/clients-registrations/openid-connect/"
	uri, err := url.Parse(public)
	if err != nil || uri.User != nil || uri.RawQuery != "" || uri.ForceQuery || uri.Fragment != "" || !strings.HasPrefix(public, prefix) {
		return "", errors.New("client management URI is outside the configured provider tenant")
	}
	suffix := strings.TrimPrefix(public, prefix)
	decoded, err := url.PathUnescape(suffix)
	if err != nil || decoded == "" || decoded == "." || decoded == ".." || strings.ContainsAny(decoded, "/\\%?#") {
		return "", errors.New("invalid client management URI")
	}
	return b.config.AdminURL + strings.TrimPrefix(public, b.config.PublicURL), nil
}

func (b *NativeBackend) validateInfo(realm string, info clientreg.ClientInformation) error {
	if info.ClientID == "" || info.RegistrationAccessToken == "" {
		return errors.New("provider returned incomplete private client management state")
	}
	if info.SubjectType != "public" {
		return errors.New("managed keycloak clients must use public subject identifiers")
	}
	_, err := b.managementURI(realm, info.RegistrationClientURI)
	return err
}

func (b *NativeBackend) Register(ctx context.Context, realm string, metadata clientreg.ClientMetadata) (clientreg.ClientInformation, error) {
	info, err := b.registrationClient(realm).Register(ctx,
		b.config.AdminURL+"/realms/"+url.PathEscape(realm)+"/clients-registrations/openid-connect", metadata)
	if err != nil {
		return clientreg.ClientInformation{}, sanitizedDCR(err)
	}
	if err := b.validateInfo(realm, info); err != nil {
		return clientreg.ClientInformation{}, err
	}
	return info, nil
}

func mergeInfo(current, observed clientreg.ClientInformation) clientreg.ClientInformation {
	if observed.RegistrationAccessToken == "" {
		observed.RegistrationAccessToken = current.RegistrationAccessToken
	}
	if observed.RegistrationClientURI == "" {
		observed.RegistrationClientURI = current.RegistrationClientURI
	}
	if observed.ClientSecret == "" {
		observed.ClientSecret = current.ClientSecret
	}
	return observed
}

func (b *NativeBackend) Read(ctx context.Context, realm string, state clientreg.ClientInformation) (clientreg.ClientInformation, error) {
	uri, err := b.managementURI(realm, state.RegistrationClientURI)
	if err != nil {
		return clientreg.ClientInformation{}, err
	}
	info, err := b.registrationClient(realm).Read(ctx, state.ClientID, uri, state.RegistrationAccessToken)
	if err != nil {
		return clientreg.ClientInformation{}, sanitizedDCR(err)
	}
	info = mergeInfo(state, info)
	if info.ClientID != state.ClientID {
		return clientreg.ClientInformation{}, errors.New("provider changed a managed client's identity")
	}
	if err := b.validateInfo(realm, info); err != nil {
		return clientreg.ClientInformation{}, err
	}
	return info, nil
}

func (b *NativeBackend) Update(ctx context.Context, realm string, state clientreg.ClientInformation, metadata clientreg.ClientMetadata) (clientreg.ClientInformation, error) {
	uri, err := b.managementURI(realm, state.RegistrationClientURI)
	if err != nil {
		return clientreg.ClientInformation{}, err
	}
	metadata.ClientID = state.ClientID
	info, err := b.registrationClient(realm).Update(ctx, uri, state.RegistrationAccessToken, metadata)
	if err != nil {
		return clientreg.ClientInformation{}, sanitizedDCR(err)
	}
	info = mergeInfo(state, info)
	if info.ClientID != state.ClientID {
		return clientreg.ClientInformation{}, errors.New("provider changed a managed client's identity")
	}
	if err := b.validateInfo(realm, info); err != nil {
		return clientreg.ClientInformation{}, err
	}
	return info, nil
}

func (b *NativeBackend) DeleteClient(ctx context.Context, realm string, state clientreg.ClientInformation) error {
	uri, err := b.managementURI(realm, state.RegistrationClientURI)
	if err != nil {
		return err
	}
	err = b.registrationClient(realm).Delete(ctx, state.ClientID, uri, state.RegistrationAccessToken)
	if clientreg.IsNotFound(err) {
		return nil
	}
	return sanitizedDCR(err)
}

type nativeClient struct {
	ID       string `json:"id"`
	ClientID string `json:"clientId"`
}

func (b *NativeBackend) ExternalClientID(ctx context.Context, realm, clientID string) (string, error) {
	var clients []nativeClient
	query := url.Values{"clientId": {clientID}, "max": {"2"}}
	if err := b.request(ctx, b.admin, http.MethodGet, b.realmURL(realm)+"/clients?"+query.Encode(), nil, &clients); err != nil {
		return "", err
	}
	if len(clients) == 0 {
		return "", ErrRemoteNotFound
	}
	if len(clients) != 1 || clients[0].ClientID != clientID || clients[0].ID == "" {
		return "", errors.New("native client lookup was ambiguous or inconsistent")
	}
	return clients[0].ID, nil
}

func (b *NativeBackend) RecoverManagementToken(ctx context.Context, realm, clientID, externalID string) (string, error) {
	actual, err := b.ExternalClientID(ctx, realm, clientID)
	if err != nil {
		return "", err
	}
	if externalID == "" || externalID != actual {
		return "", errors.New("native client identity changed; refusing management-token recovery")
	}
	var token struct {
		Value string `json:"registrationAccessToken"`
	}
	if err := b.request(ctx, b.admin, http.MethodPost, b.realmURL(realm)+"/clients/"+url.PathEscape(actual)+"/registration-access-token", nil, &token); err != nil {
		return "", err
	}
	if token.Value == "" {
		return "", errors.New("provider returned an empty management token")
	}
	return token.Value, nil
}

func (b *NativeBackend) MatchesMetadata(ctx context.Context, realm string, info clientreg.ClientInformation, wanted clientreg.ClientMetadata) (bool, error) {
	// With no logout URI, Keycloak uses the login redirect allowlist.
	if len(wanted.PostLogoutRedirectURIs) == 0 {
		wanted.PostLogoutRedirectURIs = wanted.RedirectURIs
	}
	externalID, err := b.ExternalClientID(ctx, realm, info.ClientID)
	if err != nil {
		return false, err
	}
	var native struct {
		DefaultScopes []string `json:"defaultClientScopes"`
	}
	if err := b.request(ctx, b.admin, http.MethodGet, b.realmURL(realm)+"/clients/"+url.PathEscape(externalID), nil, &native); err != nil {
		return false, err
	}
	// Keycloak's DCR response lists only optional scopes. Requested scopes
	// already assigned as realm defaults are not repeated there.
	if wanted.Scope != nil {
		scopes := slices.DeleteFunc(strings.Fields(*wanted.Scope), func(scope string) bool {
			return slices.Contains(native.DefaultScopes, scope)
		})
		value := strings.Join(scopes, " ")
		wanted.Scope = &value
	}
	return sameMetadata(wanted, info.ClientMetadata), nil
}

func sanitizedDCR(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, clientreg.ErrRegistrationNotAttempted) {
		return clientreg.ErrRegistrationNotAttempted
	}
	if response, ok := clientreg.IsHTTPError(err); ok {
		return clientreg.NewHTTPError(response.StatusCode, "", response.Operation)
	}
	return errors.New("keycloak client management request failed; remote outcome may be ambiguous")
}

func (b *NativeBackend) request(ctx context.Context, client *http.Client, method, endpoint string, body, result any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode provider request: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid provider request")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("provider request failed; remote outcome may be ambiguous")
	}
	defer response.Body.Close() //nolint:errcheck
	if response.StatusCode == http.StatusNotFound {
		return ErrRemoteNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("provider %s failed: HTTP %d", method, response.StatusCode)
	}
	if result == nil {
		return nil
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("provider response could not be read within the size limit")
	}
	if err := json.Unmarshal(data, result); err != nil || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("provider returned an invalid JSON response")
	}
	return nil
}

func clientMetadata(client pmidentityv1alpha1.OIDCClient) clientreg.ClientMetadata {
	// Keycloak treats DCR scope as optional client-scope assignments. The OIDC
	// protocol marker "openid" is implicit and is not a realm client scope.
	scopes := slices.DeleteFunc(slices.Clone(client.Scopes), func(scope string) bool { return scope == "openid" })
	scope := strings.Join(scopes, " ")
	return clientreg.ClientMetadata{
		ClientName: client.DisplayName, RedirectURIs: client.RedirectURIs,
		PostLogoutRedirectURIs: append([]string{}, client.PostLogoutRedirectURIs...),
		GrantTypes:             client.GrantTypes, ResponseTypes: client.ResponseTypes,
		TokenEndpointAuthMethod: string(client.TokenEndpointAuthMethod), Scope: &scope, SubjectType: "public",
	}
}

func sameMetadata(wanted, actual clientreg.ClientMetadata) bool {
	// Keycloak exposes the token-free "none" response alongside every code flow.
	if slices.Contains(wanted.ResponseTypes, "code") && !slices.Contains(wanted.ResponseTypes, "none") {
		actual.ResponseTypes = slices.DeleteFunc(slices.Clone(actual.ResponseTypes), func(kind string) bool { return kind == "none" })
	}
	scopes := func(scope *string) []string {
		if scope == nil {
			return nil
		}
		return strings.Fields(*scope)
	}
	equalSet := func(left, right []string) bool {
		left, right = slices.Clone(left), slices.Clone(right)
		slices.Sort(left)
		slices.Sort(right)
		return slices.Equal(left, right)
	}
	return (wanted.ClientName == "" || wanted.ClientName == actual.ClientName) &&
		wanted.SubjectType == actual.SubjectType &&
		wanted.TokenEndpointAuthMethod == actual.TokenEndpointAuthMethod &&
		equalSet(wanted.RedirectURIs, actual.RedirectURIs) &&
		equalSet(wanted.PostLogoutRedirectURIs, actual.PostLogoutRedirectURIs) &&
		equalSet(wanted.GrantTypes, actual.GrantTypes) &&
		equalSet(wanted.ResponseTypes, actual.ResponseTypes) &&
		equalSet(scopes(wanted.Scope), scopes(actual.Scope))
}
