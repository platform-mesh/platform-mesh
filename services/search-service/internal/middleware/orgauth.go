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
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/logger"
	appcontext "go.platform-mesh.io/search-service/internal/context"
	"go.platform-mesh.io/search-service/internal/service/search"
)

var issuerRegex = regexp.MustCompile(`^.*\/realms\/(.*?)\/?$`)

const defaultLocalDevelopmentOrg = "local"

type OrgContextMiddleware struct {
	validator        search.OrgAccessValidator
	localDevelopment bool
	localOrg         string
}

func NewOrgContextMiddleware(validator search.OrgAccessValidator, localDevelopment bool, localOrg string) *OrgContextMiddleware {
	localOrg = strings.TrimSpace(localOrg)
	if localOrg == "" {
		localOrg = defaultLocalDevelopmentOrg
	}
	return &OrgContextMiddleware{
		validator:        validator,
		localDevelopment: localDevelopment,
		localOrg:         localOrg,
	}
}

func (o *OrgContextMiddleware) SetRequestContext() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			log := logger.LoadLoggerFromContext(ctx)

			org := extractSubdomain(r.Host)
			if org == "" {
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}
			localHost := isLocalHost(r.Host)
			if o.localDevelopment || localHost {
				org = o.localOrg
			}

			token, err := pmcontext.GetWebTokenFromContext(ctx)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			if !o.localDevelopment && !localHost {
				authHeader, err := pmcontext.GetAuthHeaderFromContext(ctx)
				if err != nil {
					http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
					return
				}
				authHeader, err = normalizeBearerAuthHeader(authHeader)
				if err != nil {
					http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
					return
				}

				allowed, err := o.validator.ValidateTokenForOrg(ctx, authHeader, org)
				if err != nil {
					log.Error().
						Err(err).
						Str("organization", org).
						Msg("failed to validate token for org access")
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
				if !allowed {
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}
			}

			user := strings.TrimSpace(token.Mail)
			if user == "" {
				user = strings.TrimSpace(token.Subject)
			}
			if user == "" {
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			tenant, err := extractTenant(token.Issuer)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			rc := appcontext.RequestContext{Organization: org, User: user, IDMTenant: tenant}
			ctx = appcontext.WithRequestContext(ctx, rc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractSubdomain(host string) string {
	if host == "" {
		return ""
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else {
		host = strings.Split(host, ":")[0]
	}

	parts := strings.Split(host, ".")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func isLocalHost(host string) bool {
	if host == "" {
		return false
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else {
		host = strings.Split(host, ":")[0]
	}
	host = strings.TrimSpace(strings.ToLower(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func normalizeBearerAuthHeader(header string) (string, error) {
	parts := strings.Fields(strings.TrimSpace(header))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", fmt.Errorf("invalid authorization header")
	}
	return "Bearer " + parts[1], nil
}

func extractTenant(issuer string) (string, error) {
	match := issuerRegex.FindStringSubmatch(issuer)
	if len(match) < 2 || match[1] == "" {
		return "", fmt.Errorf("invalid issuer")
	}
	return match[1], nil
}
