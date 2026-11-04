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

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pmaccessproto "go.platform-mesh.io/protocol/access"
	pmaccessv1 "go.platform-mesh.io/protocol/access/v1"
	accessv1alpha1 "go.platform-mesh.io/sdk/access/apis/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newServerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	sch := runtime.NewScheme()
	if err := corev1.AddToScheme(sch); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	if err := accessv1alpha1.AddToScheme(sch); err != nil {
		t.Fatalf("add access-sdk/v1alpha1: %v", err)
	}
	return sch
}

type testIdentity struct {
	identity   *accessv1alpha1.AccessIdentity
	tokenSecr  *corev1.Secret // token Secret
	hmacSecret []byte
	tokenID    string
}

func newIdentity(t *testing.T) testIdentity {
	t.Helper()
	tok := pmaccessproto.Token{
		TokenID: "id-xyz",
		Secret:  []byte("hmac-key-material-32-bytes-------"),
	}
	identity := &accessv1alpha1.AccessIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "customer", Namespace: "default", UID: "customer-uid"},
		Status:     accessv1alpha1.AccessIdentityStatus{TokenID: tok.TokenID},
	}
	ctrlTrue := true
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "customer-access-token",
			Namespace: "default",
			Labels:    map[string]string{accessv1alpha1.TokenIDLabelKey: tok.TokenID},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: accessv1alpha1.SchemeGroupVersion.String(),
				Kind:       "AccessIdentity",
				Name:       identity.Name,
				UID:        identity.UID,
				Controller: &ctrlTrue,
			}},
		},
		Data: map[string][]byte{accessv1alpha1.AccessTokenSecretKey: []byte(tok.String())},
	}
	return testIdentity{identity: identity, tokenSecr: secret, hmacSecret: tok.Secret, tokenID: tok.TokenID}
}

func newHandler(t *testing.T, ti testIdentity) *Handler {
	t.Helper()
	sch := newServerScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(sch).
		WithObjects(ti.identity, ti.tokenSecr).
		WithStatusSubresource(&accessv1alpha1.Access{}).
		Build()
	return &Handler{
		Client:      cl,
		Scheme:      sch,
		ReplayCache: pmaccessproto.NewReplayCache(0),
	}
}

func doPost(t *testing.T, srv *httptest.Server, jwt string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/access", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestHandler_200_CreatesAccessAndKubeconfig(t *testing.T) {
	ti := newIdentity(t)
	h := newHandler(t, ti)
	srv := httptest.NewServer(h)
	defer srv.Close()

	payload := pmaccessv1.HandshakeRequest{
		KcpURL:      "https://kcp.pm.example/clusters/root",
		KcpCABundle: "ca-pem",
		SAToken:     "sa-token-value",
	}
	body, _ := json.Marshal(payload)
	jwt, err := pmaccessproto.SignCallCredential(ti.hmacSecret, ti.tokenID, body, 0)
	if err != nil {
		t.Fatal(err)
	}

	resp := doPost(t, srv, jwt, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Access CR was created.
	var accessList accessv1alpha1.AccessList
	if err := h.Client.List(context.Background(), &accessList); err != nil {
		t.Fatalf("list Access: %v", err)
	}
	if len(accessList.Items) != 1 {
		t.Fatalf("expected 1 Access, got %d", len(accessList.Items))
	}
	access := &accessList.Items[0]
	if access.Spec.IdentityRef.Name != "customer" {
		t.Errorf("Access.spec.identityRef = %q, want customer", access.Spec.IdentityRef.Name)
	}
	if access.Status.Phase != accessv1alpha1.AccessPhaseActive {
		t.Errorf("Access.status.phase = %q, want Active", access.Status.Phase)
	}

	// Kubeconfig Secret was created and is a parseable kubeconfig.
	kcName := access.Spec.KubeconfigSecretRef.Name
	kcSecret := &corev1.Secret{}
	if err := h.Client.Get(context.Background(), types.NamespacedName{Name: kcName, Namespace: "default"}, kcSecret); err != nil {
		t.Fatalf("get kubeconfig Secret %q: %v", kcName, err)
	}
	kcBytes := kcSecret.Data[kubeconfigSecretKey]
	if len(kcBytes) == 0 {
		t.Fatalf("kubeconfig Secret has no %q data", kubeconfigSecretKey)
	}
	cfg, err := clientcmd.Load(kcBytes)
	if err != nil {
		t.Fatalf("parse kubeconfig: %v", err)
	}
	if cfg.Clusters["pm-kcp"].Server != "https://kcp.pm.example/clusters/root" {
		t.Errorf("kubeconfig server mismatch")
	}
	if cfg.AuthInfos["pm-sa"].Token != "sa-token-value" {
		t.Errorf("kubeconfig SA token mismatch")
	}
}

func TestHandler_UnknownIdentity_403(t *testing.T) {
	ti := newIdentity(t)
	h := newHandler(t, ti)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// JWT signed under a different tokenID.
	body := []byte(`{"kcpURL":"x","saToken":"y"}`)
	jwt, _ := pmaccessproto.SignCallCredential([]byte("something"), "unknown", body, 0)

	resp := doPost(t, srv, jwt, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestHandler_VerifyFailure_WrongSecret_403(t *testing.T) {
	ti := newIdentity(t)
	h := newHandler(t, ti)
	srv := httptest.NewServer(h)
	defer srv.Close()

	body := []byte(`{"kcpURL":"x","saToken":"y"}`)
	jwt, _ := pmaccessproto.SignCallCredential([]byte("wrong-secret"), ti.tokenID, body, 0)

	resp := doPost(t, srv, jwt, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestHandshake(t *testing.T) {
	validPayload := func() []byte {
		b, _ := json.Marshal(pmaccessv1.HandshakeRequest{KcpURL: "https://kcp/clusters/root", KcpCABundle: "ca", SAToken: "t"})
		return b
	}

	t.Run("success returns identity and payload", func(t *testing.T) {
		ti := newIdentity(t)
		h := newHandler(t, ti)
		body := validPayload()
		jwt, err := pmaccessproto.SignCallCredential(ti.hmacSecret, ti.tokenID, body, 0)
		if err != nil {
			t.Fatal(err)
		}

		vh, err := h.doHandshake(context.Background(), jwt, body)
		if err != nil {
			t.Fatalf("handshake: %v", err)
		}
		if vh.identity.Name != "customer" {
			t.Errorf("identity.Name = %q, want customer", vh.identity.Name)
		}
		if vh.payload.KcpURL != "https://kcp/clusters/root" {
			t.Errorf("payload.KcpURL = %q, want https://kcp/clusters/root", vh.payload.KcpURL)
		}
		if vh.jti == "" {
			t.Errorf("jti is empty")
		}
	})

	t.Run("verify failure returns 403", func(t *testing.T) {
		ti := newIdentity(t)
		h := newHandler(t, ti)
		body := validPayload()
		jwt, _ := pmaccessproto.SignCallCredential([]byte("wrong-secret"), ti.tokenID, body, 0)

		_, err := h.doHandshake(context.Background(), jwt, body)
		var he *httpError
		if !errors.As(err, &he) {
			t.Fatalf("err = %v, want *httpError", err)
		}
		if he.code != http.StatusForbidden {
			t.Errorf("code = %d, want 403", he.code)
		}
	})

	t.Run("invalid body returns 403", func(t *testing.T) {
		ti := newIdentity(t)
		h := newHandler(t, ti)
		body := []byte(`{"kcpURL":"","saToken":""}`)
		jwt, _ := pmaccessproto.SignCallCredential(ti.hmacSecret, ti.tokenID, body, 0)

		_, err := h.doHandshake(context.Background(), jwt, body)
		var he *httpError
		if !errors.As(err, &he) {
			t.Fatalf("err = %v, want *httpError", err)
		}
		if he.code != http.StatusForbidden {
			t.Errorf("code = %d, want 403", he.code)
		}
	})
}

func TestCommit(t *testing.T) {
	ti := newIdentity(t)
	h := newHandler(t, ti)
	vh := &validatedHandshake{
		identity: ti.identity,
		payload:  &pmaccessv1.HandshakeRequest{KcpURL: "https://kcp/clusters/root", KcpCABundle: "ca", SAToken: "t"},
		jti:      "test-jti",
	}

	if err := h.doCommit(context.Background(), vh); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var list accessv1alpha1.AccessList
	if err := h.Client.List(context.Background(), &list); err != nil {
		t.Fatalf("list Access: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 Access, got %d", len(list.Items))
	}
	if list.Items[0].Status.Phase != accessv1alpha1.AccessPhaseActive {
		t.Errorf("phase = %q, want Active", list.Items[0].Status.Phase)
	}
}

func TestHandler_Replay_403(t *testing.T) {
	ti := newIdentity(t)
	h := newHandler(t, ti)
	srv := httptest.NewServer(h)
	defer srv.Close()

	payload := pmaccessv1.HandshakeRequest{KcpURL: "https://kcp/clusters/root", KcpCABundle: "ca", SAToken: "t"}
	body, _ := json.Marshal(payload)
	jwt, _ := pmaccessproto.SignCallCredential(ti.hmacSecret, ti.tokenID, body, 0)

	resp1 := doPost(t, srv, jwt, body)
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first dial = %d, want 200", resp1.StatusCode)
	}

	resp2 := doPost(t, srv, jwt, body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("replay dial = %d, want 403", resp2.StatusCode)
	}
}
