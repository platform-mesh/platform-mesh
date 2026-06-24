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
	"io"
	"net/http"
	"net/url"
	"strings"

	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/client-go/rest"
)

type OrgAccessValidator struct {
	http    *http.Client
	baseURL *url.URL
	log     *logger.Logger
}

func NewOrgAccessValidator(restCfg *rest.Config, log *logger.Logger) (*OrgAccessValidator, error) {
	cfg := rest.CopyConfig(restCfg)
	cfg.KeyData = nil
	cfg.CertData = nil
	cfg.KeyFile = ""
	cfg.CertFile = ""

	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("create KCP HTTP client: %w", err)
	}

	baseURL, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("parse KCP host URL: %w", err)
	}
	baseURL.Path = ""

	return &OrgAccessValidator{http: httpClient, baseURL: baseURL, log: log}, nil
}

func (v *OrgAccessValidator) ValidateTokenForOrg(ctx context.Context, authHeader, org string) (bool, error) {
	clusterPath := fmt.Sprintf("root:orgs:%s", org)
	requestURL := fmt.Sprintf("%s://%s/clusters/%s/version", v.baseURL.Scheme, v.baseURL.Host, clusterPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return false, fmt.Errorf("create KCP auth request: %w", err)
	}
	req.Header.Set("Authorization", authHeader)

	resp, err := v.http.Do(req)
	if err != nil {
		v.log.Error().
			Err(err).
			Str("organization", org).
			Str("clusterPath", clusterPath).
			Msg("KCP org token validation request failed")
		return false, fmt.Errorf("execute KCP auth request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusForbidden:
		return true, nil
	case http.StatusUnauthorized:
		v.log.Warn().
			Str("organization", org).
			Str("clusterPath", clusterPath).
			Int("statusCode", resp.StatusCode).
			Msg("KCP org token validation denied request")
		return false, nil
	default:
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		bodySnippet := strings.TrimSpace(string(bodyBytes))
		logEvt := v.log.Warn().
			Str("organization", org).
			Str("clusterPath", clusterPath).
			Int("statusCode", resp.StatusCode)
		if bodySnippet != "" {
			logEvt = logEvt.Str("responseBody", bodySnippet)
		}
		logEvt.Msg("KCP org token validation returned unexpected status")

		if strings.HasPrefix(fmt.Sprintf("%d", resp.StatusCode), "5") {
			return false, fmt.Errorf("kcp auth check failed with status %d", resp.StatusCode)
		}
		return false, nil
	}
}
