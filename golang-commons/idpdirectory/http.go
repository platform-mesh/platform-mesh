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

package idpdirectory

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"time"
)

const maxBodyBytes = 1 << 20

type protocolError struct {
	Code string `json:"code"`
}

type handler struct {
	backend   Backend
	authorize Authorizer
	log       *slog.Logger
}

// NewHandler requires verified mutual TLS with exactly one workload URI in the leaf certificate.
func NewHandler(backend Backend, authorize Authorizer, log *slog.Logger) (http.Handler, error) {
	if backend == nil || authorize == nil || log == nil {
		return nil, errors.New("directory backend, authorizer and logger are required")
	}
	return &handler{backend: backend, authorize: authorize, log: log}, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != QueryPath {
		h.writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	workload, err := workloadIdentity(r.TLS)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		h.writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var query Query
	if err := decodeJSON(r.Body, &query); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := query.Validate(); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := h.authorize(r.Context(), workload, query.Tenant); err != nil {
		h.log.WarnContext(r.Context(), "identity directory authorization rejected", "workload", workload)
		h.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	result, err := h.backend.Query(r.Context(), query)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			h.writeError(w, http.StatusNotFound, "identity_not_found")
		case errors.Is(err, ErrAmbiguous):
			h.writeError(w, http.StatusConflict, "ambiguous_identity")
		case errors.Is(err, ErrUnauthorized):
			h.writeError(w, http.StatusForbidden, "forbidden")
		default:
			// Backend errors may contain provider response bodies and personal data.
			h.log.ErrorContext(r.Context(), "identity directory backend query failed", "operation", query.Operation)
			h.writeError(w, http.StatusServiceUnavailable, "unavailable")
		}
		return
	}
	if err := result.validate(query); err != nil {
		h.log.ErrorContext(r.Context(), "identity directory backend violated response contract", "operation", query.Operation)
		h.writeError(w, http.StatusBadGateway, "invalid_provider_response")
		return
	}
	if result.Users == nil {
		result.Users = []User{}
	}
	data, err := json.Marshal(result)
	if err != nil {
		h.log.ErrorContext(r.Context(), "identity directory response encoding failed")
		h.writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if len(data) > maxBodyBytes {
		h.log.ErrorContext(r.Context(), "identity directory response exceeds size limit")
		h.writeError(w, http.StatusBadGateway, "invalid_provider_response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(data); err != nil {
		h.log.WarnContext(r.Context(), "identity directory response write failed")
	}
}

func workloadIdentity(state *tls.ConnectionState) (string, error) {
	if state == nil || len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return "", errors.New("verified client certificate required")
	}
	leaf := state.VerifiedChains[0][0]
	if len(leaf.URIs) != 1 || leaf.URIs[0].Scheme != "spiffe" || leaf.URIs[0].Host == "" ||
		leaf.URIs[0].RawQuery != "" || leaf.URIs[0].Fragment != "" || leaf.URIs[0].User != nil {
		return "", errors.New("a single SPIFFE workload identity is required")
	}
	return leaf.URIs[0].String(), nil
}

func (h *handler) writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(protocolError{Code: code}); err != nil {
		h.log.Warn("identity directory error response write failed")
	}
}

type Client struct {
	endpoint string
	http     *http.Client
}

// NewClient creates a bounded mTLS client. Redirects are rejected to prevent credential forwarding.
func NewClient(baseURL string, tlsConfig *tls.Config, timeout time.Duration) (*Client, error) {
	endpoint, err := url.Parse(baseURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") {
		return nil, errors.New("directory endpoint must be an HTTPS origin")
	}
	if tlsConfig == nil || tlsConfig.InsecureSkipVerify ||
		(len(tlsConfig.Certificates) == 0 && tlsConfig.GetClientCertificate == nil) {
		return nil, errors.New("verified mutual TLS configuration is required")
	}
	if timeout <= 0 {
		return nil, errors.New("directory timeout must be positive")
	}
	config := tlsConfig.Clone()
	if config.MinVersion < tls.VersionTLS12 {
		config.MinVersion = tls.VersionTLS12
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       config,
	}
	endpoint.Path = QueryPath
	return &Client{
		endpoint: endpoint.String(),
		http: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("directory redirects are not allowed")
			},
		},
	}, nil
}

func (c *Client) Query(ctx context.Context, query Query) (Result, error) {
	if err := query.Validate(); err != nil {
		return Result{}, err
	}
	body, err := json.Marshal(query)
	if err != nil {
		return Result{}, fmt.Errorf("encode directory query: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("create directory request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("query identity directory: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusNotFound:
			return Result{}, ErrNotFound
		case http.StatusConflict:
			return Result{}, ErrAmbiguous
		case http.StatusForbidden, http.StatusUnauthorized:
			return Result{}, ErrUnauthorized
		default:
			return Result{}, fmt.Errorf("%w: HTTP %d", ErrUnavailable, response.StatusCode)
		}
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return Result{}, errors.New("directory returned a non-JSON response")
	}
	var result Result
	if err := decodeJSON(io.LimitReader(response.Body, maxBodyBytes+1), &result); err != nil {
		return Result{}, fmt.Errorf("decode directory response: %w", err)
	}
	if err := result.validate(query); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (c *Client) CloseIdleConnections() {
	c.http.CloseIdleConnections()
}

func decodeJSON(reader io.Reader, target any) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if len(data) > maxBodyBytes {
		return errors.New("directory message exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("directory message must contain one JSON value")
	}
	return nil
}
