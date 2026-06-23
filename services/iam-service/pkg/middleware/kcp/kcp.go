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

package kcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"k8s.io/client-go/rest"

	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	"go.platform-mesh.io/iam-service/pkg/middleware/idm"
)

type Middleware struct {
	log                *logger.Logger
	tenantRetriever    idm.IDMTenantRetriever
	excludedIDMTenants []string
	restcfg            *rest.Config
}

func New(restcfg *rest.Config, excludedIDMTenants []string, tenantRetriever idm.IDMTenantRetriever, log *logger.Logger) *Middleware {
	restcfg = rest.CopyConfig(restcfg)
	restcfg.KeyData = nil
	restcfg.CertData = nil
	restcfg.KeyFile = ""
	restcfg.CertFile = ""

	return &Middleware{
		log:                log,
		tenantRetriever:    tenantRetriever,
		excludedIDMTenants: excludedIDMTenants,
		restcfg:            restcfg,
	}
}

func (m *Middleware) SetKCPUserContext() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			log := logger.LoadLoggerFromContext(ctx)

			tokenInfo, err := pmcontext.GetWebTokenFromContext(ctx)
			if err != nil {
				log.Debug().Err(err).Msg("No Token info found in context")
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			idmTenant, err := m.tenantRetriever.GetIDMTenant(tokenInfo.Issuer)
			if err != nil {
				log.Error().Err(err).Msg("Error while retrieving realm info")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			authHeader, err := pmcontext.GetAuthHeaderFromContext(ctx)
			if err != nil {
				log.Debug().Err(err).Msg("No Token info found in context")
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			// retrieve subdomain from url
			subdomain := strings.Split(r.Host, ".")[0]
			log.Debug().Str("subdmain", subdomain).Msg("processing request")

			// Create API Request against root:orgs:subdomain
			allowed, err := checkToken(ctx, authHeader, subdomain, m.restcfg)
			if err != nil {
				log.Error().Err(err).Msg("Error while checking auth")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if !allowed {
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			kctx := appcontext.KCPContext{
				OrganizationName: subdomain,
				IDMTenant:        idmTenant,
			}

			ctx = appcontext.SetKCPContext(ctx, kctx)
			log.Trace().
				Str("organization", kctx.OrganizationName).
				Msg("Added information to context was added to the context")

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func checkToken(ctx context.Context, authHeader string, subdomain string, cfg *rest.Config) (bool, error) {

	log := logger.LoadLoggerFromContext(ctx)
	clusterUrl, err := url.Parse(cfg.Host)
	if err != nil {
		log.Error().Err(errors.WithStack(err)).Msg("Error parsing KCP host URL")
	}

	if clusterUrl == nil {
		return false, errors.New("invalid KCP host URL")
	}

	clusterPath := fmt.Sprintf("root:orgs:%s", subdomain)
	requestURL := fmt.Sprintf("%s://%s/clusters/%s/version", clusterUrl.Scheme, clusterUrl.Host, clusterPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", authHeader)

	wsClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return false, err
	}

	res, err := wsClient.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close() //nolint:errcheck

	switch res.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusForbidden:
		return true, nil
	}

	return false, nil
}
