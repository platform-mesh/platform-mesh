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

package cmd

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWorkspaceRoutingIsTrustedExplicitAndDoesNotMutateSharedConfig(t *testing.T) {
	config := &rest.Config{Host: "https://kcp.example/clusters/root:another", Timeout: time.Minute}
	scoped, err := workspaceConfig(config, "root:platform-mesh-system", time.Second)
	require.NoError(t, err)
	require.Equal(t, "https://kcp.example/clusters/root:platform-mesh-system", scoped.Host)
	require.Equal(t, "https://kcp.example/clusters/root:another", config.Host)
	for _, path := range []string{"*", "../another", "root:../other", ""} {
		_, err := workspaceConfig(config, path, time.Second)
		require.Error(t, err)
	}
	_, err = workspaceConfig(&rest.Config{Host: "http://kcp.example"}, "root:system", time.Second)
	require.Error(t, err)
}

func TestProviderValidationClientUsesOnlyPrivateRuntimeSecrets(t *testing.T) {
	certPEM, keyPEM, clientRoots := validationWorkload(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		require.NotEmpty(t, req.TLS.VerifiedChains)
		require.Equal(t, "spiffe://platform-mesh.test/identity-admission", req.TLS.VerifiedChains[0][0].URIs[0].String())
		w.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientRoots}
	server.StartTLS()
	defer server.Close()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "caller", Namespace: "untrusted"},
		Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
	}
	private := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "provider-ca", Namespace: "private"},
		Data:       map[string][]byte{"ca.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})},
	}).Build()
	class := &pmidentityv1alpha1.IdentityProviderClass{Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
		ControllerName: "independent-provider.example",
		Validation: &pmidentityv1alpha1.IdentityEndpointConfig{
			URL: server.URL, TLSSecretRef: &corev1.LocalObjectReference{Name: "provider-ca"},
		},
	}}
	options := admissionOptions{namespace: "private", providerClientSecret: "caller", timeout: time.Second}
	_, err := options.providerClient(t.Context(), private, class)
	require.Error(t, err)
	trusted := secret.DeepCopy()
	trusted.ResourceVersion = ""
	trusted.Namespace = "private"
	require.NoError(t, private.Create(t.Context(), trusted))
	client, err := options.providerClient(t.Context(), private, class)
	require.NoError(t, err)
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	response, err := client.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, response.StatusCode)
	require.NoError(t, response.Body.Close())
	class.Spec.Validation.TLSSecretRef.Name = "missing-ca"
	_, err = options.providerClient(t.Context(), private, class)
	require.Error(t, err)
}

func validationWorkload(t *testing.T) ([]byte, []byte, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	workload, err := url.Parse("spiffe://platform-mesh.test/identity-admission")
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, IsCA: true, BasicConstraintsValid: true,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, URIs: []*url.URL{workload},
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	parsed, err := x509.ParseCertificate(raw)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), roots
}
