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
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/golang-commons/identity"
)

type backendFunc func(context.Context, Query) (Result, error)

func (f backendFunc) Query(ctx context.Context, query Query) (Result, error) {
	return f(ctx, query)
}

func TestDirectoryMutualTLSAndTenantAuthorization(t *testing.T) {
	tenant := Tenant{LogicalCluster: "cluster-a", Name: "org-a", UID: "tenant-uid"}
	principal := identity.Principal{Issuer: "https://issuer.example/orgs/a", Subject: "subject-a"}
	calls := 0
	backend := backendFunc(func(_ context.Context, query Query) (Result, error) {
		calls++
		require.Equal(t, tenant, query.Tenant)
		return Result{Users: []User{{Principal: principal, Email: "alice@example.org"}}}, nil
	})
	authorize := func(_ context.Context, workload string, requested Tenant) error {
		if workload != "spiffe://platform-mesh.test/iam-service" || requested != tenant {
			return ErrUnauthorized
		}
		return nil
	}
	handler, err := NewHandler(backend, authorize, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	server, config := startDirectory(t, handler)
	client, err := NewClient(server.URL, config, time.Second*5)
	require.NoError(t, err)
	t.Cleanup(client.CloseIdleConnections)

	query := Query{Tenant: tenant, Operation: LookupPrincipal, Principals: []identity.Principal{principal}}
	result, err := client.Query(t.Context(), query)
	require.NoError(t, err)
	require.Equal(t, principal, result.Users[0].Principal)

	query.Tenant.UID = "other-tenant-uid"
	_, err = client.Query(t.Context(), query)
	require.ErrorIs(t, err, ErrUnauthorized)
	require.Equal(t, 1, calls, "denied tenants must not reach provider")

	untrustedConfig := config.Clone()
	untrustedConfig.Certificates = []tls.Certificate{clientCertificate(t, "spiffe://other.test/caller")}
	untrusted, err := NewClient(server.URL, untrustedConfig, time.Second*5)
	require.NoError(t, err)
	t.Cleanup(untrusted.CloseIdleConnections)
	_, err = untrusted.Query(t.Context(), Query{Tenant: tenant, Operation: List})
	require.Error(t, err)
	require.Equal(t, 1, calls, "untrusted workloads must not reach provider")
}

func TestDirectoryRejectsUnverifiedCertificatesAndMalformedRequests(t *testing.T) {
	called := false
	h, err := NewHandler(backendFunc(func(context.Context, Query) (Result, error) {
		called = true
		return Result{}, nil
	}), func(context.Context, string, Tenant) error { return nil }, slog.Default())
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, QueryPath, strings.NewReader("{}"))
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{}}}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.False(t, called)

	for _, body := range []string{
		`{"tenant":{"logicalCluster":"c","name":"n","uid":"u"},"operation":"List","unexpected":true}`,
		`{"tenant":{"logicalCluster":"c","name":"n","uid":"u"},"operation":"List"} {}`,
		strings.Repeat(" ", maxBodyBytes+1),
	} {
		request = httptest.NewRequest(http.MethodPost, QueryPath, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		uri, parseErr := url.Parse("spiffe://platform-mesh.test/iam-service")
		require.NoError(t, parseErr)
		request.TLS = &tls.ConnectionState{
			VerifiedChains: [][]*x509.Certificate{{{URIs: []*url.URL{uri}}}},
		}
		response = httptest.NewRecorder()
		h.ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.False(t, called)
	}
}

func TestDirectoryErrorsAreExplicitAndSanitized(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{"missing", ErrNotFound, ErrNotFound},
		{"ambiguous", ErrAmbiguous, ErrAmbiguous},
		{"denied", ErrUnauthorized, ErrUnauthorized},
		{"provider failure", errors.New("provider response contains secret"), ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, err := NewHandler(backendFunc(func(context.Context, Query) (Result, error) {
				return Result{}, test.err
			}), func(context.Context, string, Tenant) error { return nil },
				slog.New(slog.NewTextHandler(io.Discard, nil)))
			require.NoError(t, err)
			server, config := startDirectory(t, h)
			client, err := NewClient(server.URL, config, time.Second*5)
			require.NoError(t, err)
			t.Cleanup(client.CloseIdleConnections)
			_, err = client.Query(t.Context(), Query{
				Tenant: Tenant{LogicalCluster: "c", Name: "n", UID: "u"}, Operation: List,
			})
			require.ErrorIs(t, err, test.want)
			require.NotContains(t, err.Error(), "contains secret")
		})
	}
}

func TestDirectoryRejectsInvalidProviderResponses(t *testing.T) {
	requested := identity.Principal{Issuer: "https://issuer.example", Subject: "a"}
	other := identity.Principal{Issuer: "https://issuer.example", Subject: "b"}
	for _, result := range []Result{
		{},
		{Users: []User{{Principal: other}}},
		{Users: []User{{Principal: requested}, {Principal: requested}}},
		{Users: []User{{Principal: requested}}, NextPageToken: "unrequested-page"},
		{Users: []User{{Principal: identity.Principal{Subject: "missing issuer"}}}},
	} {
		h, err := NewHandler(backendFunc(func(context.Context, Query) (Result, error) {
			return result, nil
		}), func(context.Context, string, Tenant) error { return nil },
			slog.New(slog.NewTextHandler(io.Discard, nil)))
		require.NoError(t, err)
		server, config := startDirectory(t, h)
		client, err := NewClient(server.URL, config, time.Second*5)
		require.NoError(t, err)
		t.Cleanup(client.CloseIdleConnections)
		_, err = client.Query(t.Context(), Query{
			Tenant:    Tenant{LogicalCluster: "c", Name: "n", UID: "u"},
			Operation: LookupPrincipal, Principals: []identity.Principal{requested},
		})
		require.ErrorIs(t, err, ErrUnavailable)
	}
}

func TestQueryValidation(t *testing.T) {
	tenant := Tenant{LogicalCluster: "c", Name: "n", UID: "u"}
	for _, query := range []Query{
		{Operation: List},
		{Tenant: tenant, Operation: "FutureOperation"},
		{Tenant: tenant, Operation: List, PageSize: MaxPageSize + 1},
		{Tenant: tenant, Operation: List, Email: "unexpected@example.org"},
		{Tenant: tenant, Operation: LookupEmail, Email: "Alice <alice@example.org>"},
		{Tenant: tenant, Operation: LookupPrincipal},
		{Tenant: tenant, Operation: BatchLookup, Principals: make([]identity.Principal, MaxBatchSize+1)},
	} {
		require.Error(t, query.Validate())
	}
	require.NoError(t, (Query{Tenant: tenant, Operation: List, PageSize: 10}).Validate())
	require.NoError(t, (Query{Tenant: tenant, Operation: LookupEmail, Email: "alice+test@example.org"}).Validate())
}

func TestDirectoryClientRejectsUnsafeConfiguration(t *testing.T) {
	cert := clientCertificate(t, "spiffe://platform-mesh.test/iam-service")
	config := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	for _, endpoint := range []string{
		"http://directory.example", "https://user:password@directory.example",
		"https://directory.example/other-path", "https://directory.example?key=secret",
	} {
		_, err := NewClient(endpoint, config, time.Second)
		require.Error(t, err)
	}
	_, err := NewClient("https://directory.example", nil, time.Second)
	require.Error(t, err)
	insecure := config.Clone()
	insecure.InsecureSkipVerify = true
	_, err = NewClient("https://directory.example", insecure, time.Second)
	require.Error(t, err)
	_, err = NewClient("https://directory.example", config, 0)
	require.Error(t, err)
}

func startDirectory(t *testing.T, handler http.Handler) (*httptest.Server, *tls.Config) {
	t.Helper()
	cert := clientCertificate(t, "spiffe://platform-mesh.test/iam-service")
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	clientRoots := x509.NewCertPool()
	clientRoots.AddCert(parsed)
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  clientRoots,
	}
	server.Config.ErrorLog = slog.NewLogLogger(slog.NewTextHandler(io.Discard, nil), slog.LevelError)
	server.StartTLS()
	t.Cleanup(server.Close)
	serverRoots := x509.NewCertPool()
	serverRoots.AddCert(server.Certificate())
	return server, &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		RootCAs:      serverRoots,
	}
}

func clientCertificate(t *testing.T, workload string) tls.Certificate {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	uri, err := url.Parse(workload)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		URIs:                  []*url.URL{uri},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}
}
