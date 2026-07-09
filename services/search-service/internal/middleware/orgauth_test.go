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

package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/jwt"

	appcontext "go.platform-mesh.io/search-service/internal/context"
)

type fakeOrgValidator struct {
	allowed bool
	err     error
	org     string
	auth    string
}

func (f *fakeOrgValidator) ValidateTokenForOrg(_ context.Context, authHeader, org string) (bool, error) {
	f.org = org
	f.auth = authHeader
	return f.allowed, f.err
}

func TestSetRequestContextSuccessUsesMailFallbackToSub(t *testing.T) {
	validator := &fakeOrgValidator{allowed: true}
	mw := NewOrgContextMiddleware(validator, false, "local")

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=test", nil)
	req.Host = "acme.platform-mesh.io:8443"

	ctx := pmcontext.AddAuthHeaderToContext(req.Context(), "Bearer abc")
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, jwt.WebToken{
		IssuerAttributes: jwt.IssuerAttributes{
			Issuer:  "https://idp.example.org/auth/realms/acme-tenant",
			Subject: "subject-user",
		},
		ParsedAttributes: jwt.ParsedAttributes{Mail: ""},
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc, err := appcontext.GetRequestContext(r.Context())
		if err != nil {
			t.Fatalf("request context missing: %v", err)
		}
		if rc.Organization != "acme" {
			t.Fatalf("unexpected org: %s", rc.Organization)
		}
		if rc.User != "subject-user" {
			t.Fatalf("expected subject fallback user, got %s", rc.User)
		}
		if rc.IDMTenant != "acme-tenant" {
			t.Fatalf("unexpected tenant: %s", rc.IDMTenant)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mw.SetRequestContext()(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if validator.org != "acme" {
		t.Fatalf("expected org acme in validator, got %s", validator.org)
	}
	if validator.auth != "Bearer abc" {
		t.Fatalf("expected auth header passed to validator")
	}
}

func TestSetRequestContextForbiddenWhenOrgCheckFails(t *testing.T) {
	validator := &fakeOrgValidator{allowed: false}
	mw := NewOrgContextMiddleware(validator, false, "local")

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=test", nil)
	req.Host = "acme.platform-mesh.io"
	ctx := pmcontext.AddAuthHeaderToContext(req.Context(), "Bearer abc")
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, jwt.WebToken{
		IssuerAttributes: jwt.IssuerAttributes{
			Issuer:  "https://idp.example.org/auth/realms/acme-tenant",
			Subject: "user",
		},
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	mw.SetRequestContext()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatalf("next handler must not be called")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestSetRequestContextReturns500OnValidatorError(t *testing.T) {
	validator := &fakeOrgValidator{err: errors.New("boom")}
	mw := NewOrgContextMiddleware(validator, false, "local")

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=test", nil)
	req.Host = "acme.platform-mesh.io"
	ctx := pmcontext.AddAuthHeaderToContext(req.Context(), "Bearer abc")
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, jwt.WebToken{
		IssuerAttributes: jwt.IssuerAttributes{
			Issuer:  "https://idp.example.org/auth/realms/acme-tenant",
			Subject: "user",
		},
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	mw.SetRequestContext()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatalf("next handler must not be called")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestSetRequestContextReturns401ForInvalidTokenContext(t *testing.T) {
	validator := &fakeOrgValidator{allowed: true}
	mw := NewOrgContextMiddleware(validator, false, "local")

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=test", nil)
	req.Host = "acme.platform-mesh.io"
	req = req.WithContext(pmcontext.AddAuthHeaderToContext(req.Context(), "Bearer abc"))

	rr := httptest.NewRecorder()
	mw.SetRequestContext()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatalf("next handler must not be called")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestSetRequestContextReturns401ForInvalidIssuer(t *testing.T) {
	validator := &fakeOrgValidator{allowed: true}
	mw := NewOrgContextMiddleware(validator, false, "local")

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=test", nil)
	req.Host = "acme.platform-mesh.io"
	ctx := pmcontext.AddAuthHeaderToContext(req.Context(), "Bearer abc")
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, jwt.WebToken{
		IssuerAttributes: jwt.IssuerAttributes{
			Issuer:  "https://idp.example.org/no-realms-segment",
			Subject: "user",
		},
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	mw.SetRequestContext()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatalf("next handler must not be called")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestSetRequestContextLocalhostOverridesOrgAndBypassesValidator(t *testing.T) {
	validator := &fakeOrgValidator{allowed: false}
	mw := NewOrgContextMiddleware(validator, false, "local-org-test")

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=test", nil)
	req.Host = "localhost:8443"

	ctx := pmcontext.AddAuthHeaderToContext(req.Context(), "bearer\tabc")
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, jwt.WebToken{
		IssuerAttributes: jwt.IssuerAttributes{
			Issuer:  "https://idp.example.org/auth/realms/acme-tenant",
			Subject: "subject-user",
		},
		ParsedAttributes: jwt.ParsedAttributes{Mail: "user@example.org"},
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc, err := appcontext.GetRequestContext(r.Context())
		if err != nil {
			t.Fatalf("request context missing: %v", err)
		}
		if rc.Organization != "local-org-test" {
			t.Fatalf("unexpected org: %s", rc.Organization)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mw.SetRequestContext()(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if validator.org != "" || validator.auth != "" {
		t.Fatalf("validator must not be called for localhost requests")
	}
}

func TestSetRequestContextBypassesValidatorAndUsesConfiguredOrgInLocalDevelopmentMode(t *testing.T) {
	validator := &fakeOrgValidator{allowed: false}
	mw := NewOrgContextMiddleware(validator, true, "local-org-test")

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=test", nil)
	req.Host = "search.portal.localhost"

	ctx := pmcontext.AddAuthHeaderToContext(req.Context(), "Bearer abc")
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, jwt.WebToken{
		IssuerAttributes: jwt.IssuerAttributes{
			Issuer:  "https://idp.example.org/auth/realms/acme-tenant",
			Subject: "subject-user",
		},
		ParsedAttributes: jwt.ParsedAttributes{Mail: "user@example.org"},
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc, err := appcontext.GetRequestContext(r.Context())
		if err != nil {
			t.Fatalf("request context missing: %v", err)
		}
		if rc.Organization != "local-org-test" {
			t.Fatalf("unexpected org: %s", rc.Organization)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mw.SetRequestContext()(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if validator.org != "" || validator.auth != "" {
		t.Fatalf("validator must not be called in local development mode")
	}
}

func TestSetRequestContextReturns401ForMalformedAuthorizationHeader(t *testing.T) {
	validator := &fakeOrgValidator{allowed: true}
	mw := NewOrgContextMiddleware(validator, false, "local")

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=test", nil)
	req.Host = "acme.platform-mesh.io"

	ctx := pmcontext.AddAuthHeaderToContext(req.Context(), "abc")
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, jwt.WebToken{
		IssuerAttributes: jwt.IssuerAttributes{
			Issuer:  "https://idp.example.org/auth/realms/acme-tenant",
			Subject: "subject-user",
		},
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	mw.SetRequestContext()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatalf("next handler must not be called")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if validator.org != "" || validator.auth != "" {
		t.Fatalf("validator must not be called on malformed auth header")
	}
}

func TestNewOrgContextMiddlewareFallsBackToDefaultLocalOrg(t *testing.T) {
	validator := &fakeOrgValidator{allowed: false}
	mw := NewOrgContextMiddleware(validator, false, "")

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=test", nil)
	req.Host = "localhost:8443"

	ctx := pmcontext.AddAuthHeaderToContext(req.Context(), "Bearer abc")
	ctx = context.WithValue(ctx, keys.WebTokenCtxKey, jwt.WebToken{
		IssuerAttributes: jwt.IssuerAttributes{
			Issuer:  "https://idp.example.org/auth/realms/acme-tenant",
			Subject: "subject-user",
		},
		ParsedAttributes: jwt.ParsedAttributes{Mail: "user@example.org"},
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc, err := appcontext.GetRequestContext(r.Context())
		if err != nil {
			t.Fatalf("request context missing: %v", err)
		}
		if rc.Organization != defaultLocalDevelopmentOrg {
			t.Fatalf("unexpected org: %s", rc.Organization)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mw.SetRequestContext()(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}
