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

// Package directory implements the neutral directory protocol against Keycloak.
package directory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go.platform-mesh.io/golang-commons/identity"
	"go.platform-mesh.io/golang-commons/idpdirectory"
)

type TenantConfiguration struct {
	Realm     string
	IssuerURL string
}

// TenantResolver must verify the KCP logical cluster, object name and UID and
// return only a tenant bound to this provider. The caller cannot select a realm.
type TenantResolver interface {
	Resolve(context.Context, idpdirectory.Tenant) (TenantConfiguration, error)
}

type Backend struct {
	baseURL  string
	client   *http.Client
	resolver TenantResolver
}

func New(baseURL string, client *http.Client, resolver TenantResolver) (*Backend, error) {
	endpoint, err := url.Parse(baseURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" ||
		endpoint.Fragment != "" || !validScheme(endpoint) {
		return nil, errors.New("keycloak directory requires an HTTPS base URL (HTTP allowed only on loopback)")
	}
	if client == nil || client.Timeout <= 0 || resolver == nil {
		return nil, errors.New("keycloak directory requires a bounded authenticated HTTP client and tenant resolver")
	}
	bounded := *client
	bounded.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("keycloak directory redirects are not allowed")
	}
	return &Backend{baseURL: strings.TrimSuffix(baseURL, "/"), client: &bounded, resolver: resolver}, nil
}

func validScheme(endpoint *url.URL) bool {
	if endpoint.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(endpoint.Hostname())
	return endpoint.Scheme == "http" && ip != nil && ip.IsLoopback()
}

func (b *Backend) Query(ctx context.Context, query idpdirectory.Query) (idpdirectory.Result, error) {
	if err := query.Validate(); err != nil {
		return idpdirectory.Result{}, err
	}
	tenant, err := b.resolver.Resolve(ctx, query.Tenant)
	if err != nil {
		return idpdirectory.Result{}, fmt.Errorf("resolve directory tenant: %w", err)
	}
	if tenant.Realm == "" || tenant.Realm == "." || tenant.Realm == ".." ||
		tenant.Realm == "master" || strings.ContainsAny(tenant.Realm, "/\\?#") {
		return idpdirectory.Result{}, errors.New("directory tenant has an invalid or reserved realm")
	}
	if _, err := (identity.Principal{Issuer: tenant.IssuerURL, Subject: "issuer-validation"}).Username(); err != nil {
		return idpdirectory.Result{}, fmt.Errorf("invalid directory tenant issuer: %w", err)
	}
	path := b.baseURL + "/admin/realms/" + url.PathEscape(tenant.Realm) + "/users"
	switch query.Operation {
	case idpdirectory.List:
		return b.list(ctx, query, tenant, path)
	case idpdirectory.LookupEmail:
		var users []user
		params := url.Values{"email": {query.Email}, "exact": {"true"}, "max": {"2"}}
		if err := b.get(ctx, path+"?"+params.Encode(), &users); err != nil {
			return idpdirectory.Result{}, err
		}
		if users == nil {
			return idpdirectory.Result{}, errors.New("keycloak email lookup returned null instead of a user list")
		}
		if len(users) > 1 {
			return idpdirectory.Result{}, idpdirectory.ErrAmbiguous
		}
		if len(users) == 0 {
			return idpdirectory.Result{}, idpdirectory.ErrNotFound
		}
		if users[0].Email != query.Email {
			return idpdirectory.Result{}, errors.New("keycloak exact email lookup returned a different address")
		}
		result, err := convert(users[0], tenant.IssuerURL)
		if err != nil {
			return idpdirectory.Result{}, err
		}
		return idpdirectory.Result{Users: []idpdirectory.User{result}}, nil
	case idpdirectory.LookupPrincipal, idpdirectory.BatchLookup:
		result := idpdirectory.Result{Users: make([]idpdirectory.User, 0, len(query.Principals))}
		for _, principal := range query.Principals {
			if principal.Issuer != tenant.IssuerURL {
				return idpdirectory.Result{}, idpdirectory.ErrUnauthorized
			}
			if principal.Subject == "." || principal.Subject == ".." || strings.ContainsAny(principal.Subject, "/\\") {
				return idpdirectory.Result{}, errors.New("subject is not a supported keycloak user ID")
			}
			var found user
			if err := b.get(ctx, path+"/"+url.PathEscape(principal.Subject), &found); err != nil {
				if errors.Is(err, idpdirectory.ErrNotFound) && query.Operation == idpdirectory.BatchLookup {
					continue
				}
				return idpdirectory.Result{}, err
			}
			entry, err := convert(found, tenant.IssuerURL)
			if err != nil {
				return idpdirectory.Result{}, err
			}
			if entry.Principal != principal {
				return idpdirectory.Result{}, errors.New("keycloak returned an unrequested user")
			}
			result.Users = append(result.Users, entry)
		}
		return result, nil
	default:
		return idpdirectory.Result{}, fmt.Errorf("unsupported directory operation %q", query.Operation)
	}
}

type page struct {
	TenantUID string `json:"tenantUid"`
	Offset    int    `json:"offset"`
}

func (b *Backend) list(ctx context.Context, query idpdirectory.Query, tenant TenantConfiguration, path string) (idpdirectory.Result, error) {
	cursor := page{TenantUID: query.Tenant.UID}
	if query.PageToken != "" {
		data, err := base64.RawURLEncoding.DecodeString(query.PageToken)
		if err != nil {
			return idpdirectory.Result{}, errors.New("invalid directory cursor")
		}
		if err := json.Unmarshal(data, &cursor); err != nil || cursor.TenantUID != query.Tenant.UID ||
			cursor.Offset < 0 || cursor.Offset > 1_000_000_000 {
			return idpdirectory.Result{}, errors.New("invalid or cross-tenant directory cursor")
		}
	}
	size := query.PageSize
	if size == 0 {
		size = idpdirectory.MaxPageSize
	}
	params := url.Values{"first": {strconv.Itoa(cursor.Offset)}, "max": {strconv.Itoa(size + 1)}}
	var users []user
	if err := b.get(ctx, path+"?"+params.Encode(), &users); err != nil {
		return idpdirectory.Result{}, err
	}
	if users == nil {
		return idpdirectory.Result{}, errors.New("keycloak directory returned null instead of a user list")
	}
	if len(users) > size+1 {
		return idpdirectory.Result{}, errors.New("keycloak returned more users than requested")
	}
	result := idpdirectory.Result{Users: make([]idpdirectory.User, 0, min(len(users), size))}
	if len(users) > size {
		cursor.Offset += size
		data, err := json.Marshal(cursor)
		if err != nil {
			return idpdirectory.Result{}, fmt.Errorf("encode directory cursor: %w", err)
		}
		result.NextPageToken = base64.RawURLEncoding.EncodeToString(data)
		users = users[:size]
	}
	for _, entry := range users {
		converted, err := convert(entry, tenant.IssuerURL)
		if err != nil {
			return idpdirectory.Result{}, err
		}
		result.Users = append(result.Users, converted)
	}
	return result, nil
}

type user struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
}

func convert(entry user, issuer string) (idpdirectory.User, error) {
	principal := identity.Principal{Issuer: issuer, Subject: entry.ID}
	if _, err := principal.Username(); err != nil {
		return idpdirectory.User{}, errors.New("keycloak returned an invalid user identity")
	}
	return idpdirectory.User{
		Principal: principal, Email: entry.Email, EmailVerified: entry.EmailVerified,
		GivenName: entry.FirstName, FamilyName: entry.LastName,
	}, nil
}

func (b *Backend) get(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create Keycloak directory request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	response, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("request Keycloak directory: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck
	if response.StatusCode == http.StatusNotFound {
		return idpdirectory.ErrNotFound
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("keycloak directory returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return fmt.Errorf("read Keycloak directory response: %w", err)
	}
	if len(data) > 1<<20 {
		return errors.New("keycloak directory response exceeds size limit")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode Keycloak directory response: %w", err)
	}
	return nil
}
