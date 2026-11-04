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

package accessgrant

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"
	pmaccessproto "go.platform-mesh.io/protocol/access"
	pmaccessv1 "go.platform-mesh.io/protocol/access/v1"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newHandshakeReconciler builds an h with stub dependencies: a fixed
// kcp external URL, a static CA bundle, and a recording invalidate
// callback whose last argument is exposed via *invalidated.
func newHandshakeReconciler(invalidated *string) *handshakeReconciler {
	return &handshakeReconciler{
		kcpExternalURL: "https://kcp.pm.example",
		readCABundle:   func() ([]byte, error) { return []byte("stub-ca-bundle"), nil },
		invalidateVer: func(u string) {
			if invalidated != nil {
				*invalidated = u
			}
		},
	}
}

func TestHandshake_200_MarksConnected(t *testing.T) {
	var gotBody pmaccessv1.HandshakeRequest
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fx := newFixture(t, srv.URL)
	h := newHandshakeReconciler(nil)

	if _, err := h.reconcile(testCtx(t),srv.Client(), fx.grant, "test-cluster", pmaccessproto.V1, fx.token, "stub-sa-token"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	cond := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != pmaccessv1alpha1.AccessGrantHandshakeSucceededReason {
		t.Errorf("Connected condition = %+v, want True/HandshakeSucceeded", cond)
	}
	if len(gotAuth) < len("Bearer ") || gotAuth[:7] != "Bearer " {
		t.Errorf("Authorization header = %q, want Bearer <jwt>", gotAuth)
	}
	if gotBody.KcpURL != "https://kcp.pm.example/clusters/test-cluster" {
		t.Errorf("kcpURL = %q, want https://kcp.pm.example/clusters/test-cluster", gotBody.KcpURL)
	}
	if gotBody.SAToken != "stub-sa-token" {
		t.Errorf("saToken = %q, want stub-sa-token", gotBody.SAToken)
	}
	if gotBody.KcpCABundle != "stub-ca-bundle" {
		t.Errorf("kcpCABundle = %q, want stub-ca-bundle", gotBody.KcpCABundle)
	}
}

func TestHandshake_DialFailure_InvalidatesCache(t *testing.T) {
	// Close the server immediately so the POST fails to dial.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()

	fx := newFixture(t, url)
	var invalidated string
	h := newHandshakeReconciler(&invalidated)

	res, err := h.reconcile(testCtx(t),http.DefaultClient, fx.grant, "test-cluster", pmaccessproto.V1, fx.token, "stub-sa-token")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != retryUnreachable {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, retryUnreachable)
	}
	if invalidated != url {
		t.Errorf("invalidateVer called with %q, want %q", invalidated, url)
	}
	cond := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted)
	if cond == nil || cond.Reason != pmaccessv1alpha1.AccessGrantAccessURLUnreachableReason {
		t.Errorf("Connected condition = %+v, want AccessURLUnreachable", cond)
	}
}

func TestHandshake_UnexpectedStatus_UnreachableReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	fx := newFixture(t, srv.URL)
	h := newHandshakeReconciler(nil)

	res, err := h.reconcile(testCtx(t),srv.Client(), fx.grant, "test-cluster", pmaccessproto.V1, fx.token, "stub-sa-token")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != retryOther {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, retryOther)
	}
	cond := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted)
	if cond == nil || cond.Reason != pmaccessv1alpha1.AccessGrantAccessURLUnreachableReason {
		t.Errorf("Connected condition = %+v, want AccessURLUnreachable", cond)
	}
}
