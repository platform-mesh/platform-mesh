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

package admission

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	webhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestHTTPValidationRejectsMissingRequestUIDBeforeOpeningTransport(t *testing.T) {
	called := false
	dispatch := &HTTPDispatcher{
		Timeout: time.Second,
		ClientForClass: func(context.Context, *pmidentityv1alpha1.IdentityProviderClass) (*http.Client, error) {
			called = true
			return nil, errors.New("unexpected transport configuration")
		},
	}
	class := &pmidentityv1alpha1.IdentityProviderClass{Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
		Validation: &pmidentityv1alpha1.IdentityEndpointConfig{URL: "https://validator.example/admit"},
	}}
	_, err := dispatch.Validate(t.Context(), class, webhookadmission.Request{})
	require.ErrorContains(t, err, "request UID")
	require.False(t, called)
}

func TestHTTPValidationRequiresAuthenticatedMatchingNonmutatingReview(t *testing.T) {
	tests := []struct {
		name string
		edit func(*admissionv1.AdmissionReview)
	}{
		{"valid denial", func(*admissionv1.AdmissionReview) {}},
		{"wrong request UID", func(r *admissionv1.AdmissionReview) { r.Response.UID = "another-request" }},
		{"wrong version", func(r *admissionv1.AdmissionReview) { r.APIVersion = "admission.k8s.io/v1beta1" }},
		{"missing response", func(r *admissionv1.AdmissionReview) { r.Response = nil }},
		{"mutating response", func(r *admissionv1.AdmissionReview) { r.Response.Patch = []byte(`[{"op":"remove","path":"/spec"}]`) }},
		{"oversized response", func(r *admissionv1.AdmissionReview) {
			r.Response.Result.Message = strings.Repeat("x", maxReviewBytes+1)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certificate, clientRoots := validationClientCertificate(t)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				require.NotEmpty(t, req.TLS.VerifiedChains)
				require.Equal(t, http.MethodPost, req.Method)
				var review admissionv1.AdmissionReview
				require.NoError(t, json.NewDecoder(req.Body).Decode(&review))
				require.Equal(t, "request-uid", string(review.Request.UID))
				result := admissionv1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{APIVersion: admissionv1.SchemeGroupVersion.String(), Kind: "AdmissionReview"},
					Response: &admissionv1.AdmissionResponse{UID: review.Request.UID, Allowed: false, Result: &metav1.Status{Message: "provider restriction"}},
				}
				tt.edit(&result)
				require.NoError(t, json.NewEncoder(w).Encode(result))
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientRoots}
			server.StartTLS()
			defer server.Close()
			client := server.Client()
			client.Timeout = time.Second
			transport := client.Transport.(*http.Transport)
			transport.TLSClientConfig.Certificates = []tls.Certificate{certificate}
			defer client.CloseIdleConnections()
			dispatch := &HTTPDispatcher{
				Timeout: time.Second,
				ClientForClass: func(context.Context, *pmidentityv1alpha1.IdentityProviderClass) (*http.Client, error) {
					return client, nil
				},
			}
			class := &pmidentityv1alpha1.IdentityProviderClass{Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
				Validation: &pmidentityv1alpha1.IdentityEndpointConfig{URL: server.URL},
			}}
			response, err := dispatch.Validate(t.Context(), class, webhookadmission.Request{AdmissionRequest: admissionv1.AdmissionRequest{UID: "request-uid"}})
			if tt.name == "valid denial" {
				require.NoError(t, err)
				require.False(t, response.Allowed)
				require.Equal(t, "provider restriction", response.Result.Message)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestHTTPValidationRejectsRedirectsAndUntrustedTransport(t *testing.T) {
	followed := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/followed" {
			followed = true
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/followed", http.StatusFound)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	certificate, _ := validationClientCertificate(t)
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.Certificates = []tls.Certificate{certificate}
	defer client.CloseIdleConnections()
	dispatch := &HTTPDispatcher{
		Timeout: time.Second,
		ClientForClass: func(context.Context, *pmidentityv1alpha1.IdentityProviderClass) (*http.Client, error) {
			return client, nil
		},
	}
	class := &pmidentityv1alpha1.IdentityProviderClass{Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
		Validation: &pmidentityv1alpha1.IdentityEndpointConfig{URL: server.URL},
	}}
	request := webhookadmission.Request{AdmissionRequest: admissionv1.AdmissionRequest{UID: "request-uid"}}
	_, err := dispatch.Validate(t.Context(), class, request)
	require.Error(t, err)
	require.False(t, followed)
	transport.TLSClientConfig.Certificates = nil
	_, err = dispatch.Validate(t.Context(), class, request)
	require.ErrorContains(t, err, "mutual TLS")
	dispatch.ClientForClass = func(context.Context, *pmidentityv1alpha1.IdentityProviderClass) (*http.Client, error) {
		return nil, errors.New("private transport configuration")
	}
	_, err = dispatch.Validate(t.Context(), class, request)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private transport configuration")
}

func validationClientCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, IsCA: true, BasicConstraintsValid: true,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	parsed, err := x509.ParseCertificate(raw)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{raw}, PrivateKey: key}, roots
}
