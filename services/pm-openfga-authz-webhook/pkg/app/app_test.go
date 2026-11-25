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

package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/config"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/handler/mocks"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func orgsStoreResponse() *openfgav1.ListStoresResponse {
	return &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{{Id: "01ABC", Name: OrgsStoreName}},
	}
}

func TestWaitForStore_ReturnsStoreID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		expect func(*mocks.OpenFGAServiceClient)
		wantID string
	}{
		{
			name: "store exists",
			expect: func(fga *mocks.OpenFGAServiceClient) {
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(orgsStoreResponse(), nil)
			},
			wantID: "01ABC",
		},
		// The cold start case where the authz webhook may start before
		// the pm-openfga-operator has created the store.
		{
			name: "store appears after an empty answer and a list error",
			expect: func(fga *mocks.OpenFGAServiceClient) {
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{}, nil).Once()
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(orgsStoreResponse(), nil).Once()
			},
			wantID: "01ABC",
		},
		{
			name: "store of another name is ignored",
			expect: func(fga *mocks.OpenFGAServiceClient) {
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{{Id: "01OTHER", Name: "some-other-store"}},
				}, nil).Once()
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(orgsStoreResponse(), nil).Once()
			},
			wantID: "01ABC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fga := mocks.NewOpenFGAServiceClient(t)
			tt.expect(fga)

			storeID, err := WaitForStore(t.Context(), fga, OrgsStoreName, time.Minute)

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, storeID)
		})
	}
}

func TestWaitForStore_StoreNeverAppears(t *testing.T) {
	t.Parallel()

	fga := mocks.NewOpenFGAServiceClient(t)
	fga.EXPECT().ListStores(mock.Anything, mock.Anything).Maybe().
		Run(func(args mock.Arguments) {
			req, ok := args.Get(1).(*openfgav1.ListStoresRequest)
			require.True(t, ok)
			assert.Equal(t, OrgsStoreName, req.GetName(), "the wait must ask OpenFGA for the orgs store by name")
		}).
		Return(&openfgav1.ListStoresResponse{}, nil)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	storeID, err := WaitForStore(ctx, fga, OrgsStoreName, 0)

	require.Error(t, err)
	assert.Empty(t, storeID)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), OrgsStoreName, "the failure must name the store it waited for")
}

func TestWaitForStore_Timeout(t *testing.T) {
	t.Parallel()

	fga := mocks.NewOpenFGAServiceClient(t)
	fga.EXPECT().ListStores(mock.Anything, mock.Anything).Maybe().Return(&openfgav1.ListStoresResponse{}, nil)

	storeID, err := WaitForStore(t.Context(), fga, OrgsStoreName, 100*time.Millisecond)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, storeID)
	require.NoError(t, t.Context().Err(), "the timeout must not cancel the caller's context")
}

const workspacePrefix = "/clusters/root:platform-mesh-system"

// newFakeKCP serves just enough of the kcp API for the APIExport providers to
// start: discovery and an empty list and watch of APIExportEndpointSlices.
func newFakeKCP(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
		}

		switch strings.TrimPrefix(r.URL.Path, workspacePrefix) {
		case "/api":
			writeJSON(map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
		case "/api/v1":
			writeJSON(map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []any{}})
		case "/apis":
			version := map[string]any{"groupVersion": "apis.kcp.io/v1alpha1", "version": "v1alpha1"}
			writeJSON(map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{
				map[string]any{"name": "apis.kcp.io", "versions": []any{version}, "preferredVersion": version},
			}})
		case "/apis/apis.kcp.io/v1alpha1":
			writeJSON(map[string]any{"kind": "APIResourceList", "apiVersion": "v1", "groupVersion": "apis.kcp.io/v1alpha1", "resources": []any{
				map[string]any{"name": "apiexportendpointslices", "singularName": "apiexportendpointslice", "namespaced": false, "kind": "APIExportEndpointSlice", "verbs": []string{"get", "list", "watch"}},
			}})
		case "/apis/apis.kcp.io/v1alpha1/apiexportendpointslices":
			if r.URL.Query().Get("watch") != "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			writeJSON(map[string]any{"kind": "APIExportEndpointSliceList", "apiVersion": "apis.kcp.io/v1alpha1", "metadata": map[string]any{"resourceVersion": "1"}, "items": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		server.CloseClientConnections()
		server.Close()
	})

	return server
}

// writeServingCert writes a self-signed certificate for 127.0.0.1 as tls.crt
// and tls.key into a new directory and returns the directory and a pool that
// trusts the certificate.
func writeServingCert(t *testing.T) (string, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "pm-openfga-authz-webhook"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	dir := t.TempDir()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.crt"), certPEM, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.key"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(certPEM))

	return dir, pool
}

func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close() //nolint:errcheck

	return l.Addr().(*net.TCPAddr).Port
}

// TestRun runs the webhook the way an embedding process does: an injected
// OpenFGA client, metrics and health probes disabled, the webhook server bound
// to the loopback interface with a certificate from a directory.
func TestRun(t *testing.T) {
	kcp := newFakeKCP(t)
	certDir, pool := writeServingCert(t)
	port := freePort(t)

	fga := mocks.NewOpenFGAServiceClient(t)
	fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{}, nil).Once()
	fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(orgsStoreResponse(), nil).Once()

	cfg := config.New()
	cfg.MetricsBindAddress = "0"
	cfg.HealthProbeBindAddress = "0"
	cfg.StoreWaitTimeout = 0
	cfg.Webhook.Host = "127.0.0.1"
	cfg.Webhook.Port = port
	cfg.Webhook.CertDir = certDir

	hostBefore := kcp.URL + workspacePrefix
	kcpCfg := &rest.Config{Host: hostBefore}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, kcpCfg, cfg, Deps{OpenFGA: fga})
	}()

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
	}
	review := func(c *assert.CollectT, spec authorizationv1.SubjectAccessReviewSpec) (*http.Response, authorizationv1.SubjectAccessReview) {
		body, err := json.Marshal(authorizationv1.SubjectAccessReview{
			TypeMeta: metav1.TypeMeta{APIVersion: "authorization.k8s.io/v1", Kind: "SubjectAccessReview"},
			Spec:     spec,
		})
		require.NoError(t, err)

		res, err := client.Post(fmt.Sprintf("https://127.0.0.1:%d%s", port, AuthzPath), "application/json", bytes.NewReader(body))
		if !assert.NoError(c, err) {
			return nil, authorizationv1.SubjectAccessReview{}
		}
		defer res.Body.Close() //nolint:errcheck

		var sar authorizationv1.SubjectAccessReview
		if res.StatusCode == http.StatusOK {
			assert.NoError(c, json.NewDecoder(res.Body).Decode(&sar))
		}
		return res, sar
	}

	// Discovery paths are allowed for everybody.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		select {
		case err := <-done:
			done <- err
			assert.Fail(c, "Run returned early", "%v", err)
			return
		default:
		}

		res, sar := review(c, authorizationv1.SubjectAccessReviewSpec{
			User:                  "alice@example.com",
			NonResourceAttributes: &authorizationv1.NonResourceAttributes{Path: "/api", Verb: "get"},
		})
		if res == nil {
			return
		}
		assert.Equal(c, http.StatusOK, res.StatusCode)
		assert.True(c, sar.Status.Allowed)
	}, 30*time.Second, 100*time.Millisecond, "the webhook must come up")

	// Without the cluster name extra the webhook has no opinion: the request is
	// neither allowed nor denied.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, sar := review(c, authorizationv1.SubjectAccessReviewSpec{
			User:               "system:admin",
			Groups:             []string{"system:kcp:admin"},
			ResourceAttributes: &authorizationv1.ResourceAttributes{Verb: "get", Resource: "configmaps", Name: "x"},
		})
		if res == nil {
			return
		}
		assert.Equal(c, http.StatusOK, res.StatusCode)
		assert.False(c, sar.Status.Allowed)
		assert.False(c, sar.Status.Denied)
	}, 10*time.Second, 100*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "Run must return nil when its context is done")
	case <-time.After(60 * time.Second):
		t.Fatal("Run did not return after its context was done")
	}

	assert.Equal(t, hostBefore, kcpCfg.Host, "the caller's kcp config must not be modified")
	assert.Nil(t, kcpCfg.WrapTransport, "the caller's kcp config must not be modified")
}

func TestRun_ReturnsErrors(t *testing.T) {
	t.Run("missing arguments", func(t *testing.T) {
		require.Error(t, Run(t.Context(), nil, config.New(), Deps{}))
		require.Error(t, Run(t.Context(), &rest.Config{}, nil, Deps{}))
	})

	t.Run("kcp not reachable", func(t *testing.T) {
		err := Run(t.Context(), &rest.Config{Host: "http://127.0.0.1:1" + workspacePrefix}, config.New(), Deps{})
		require.ErrorContains(t, err, "unable to construct cluster provider")
	})

	t.Run("store never appears", func(t *testing.T) {
		fga := mocks.NewOpenFGAServiceClient(t)
		fga.EXPECT().ListStores(mock.Anything, mock.Anything).Maybe().Return(&openfgav1.ListStoresResponse{}, nil)

		cfg := config.New()
		cfg.MetricsBindAddress = "0"
		cfg.HealthProbeBindAddress = "0"
		cfg.StoreWaitTimeout = 100 * time.Millisecond

		err := Run(t.Context(), &rest.Config{Host: newFakeKCP(t).URL + workspacePrefix}, cfg, Deps{OpenFGA: fga})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorContains(t, err, OrgsStoreName)
	})
}
