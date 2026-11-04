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

package access

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmaccessproto "go.platform-mesh.io/protocol/access"
	pmaccessv1 "go.platform-mesh.io/protocol/access/v1"
	accessv1alpha1 "go.platform-mesh.io/sdk/access/apis/v1alpha1"
	"go.platform-mesh.io/sdk/access/pkg/server"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// testHandlerDeliversKubeconfig: an end-to-end /v1/access handshake
// against a real AccessIdentity minted by the identity controller.
// The handler verifies the JWT against the identity's HMAC, creates an
// Access CR, writes the kubeconfig Secret, and marks the Access Active.
func testHandlerDeliversKubeconfig(t *testing.T, e *env) {
	ctx := t.Context()
	const identityName = "test-handler-identity"

	// 1. Mint an AccessIdentity + wait for its access-token Secret.
	require.NoError(t, e.cl.Create(ctx, &accessv1alpha1.AccessIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: identityName, Namespace: testNamespace},
	}))

	var tokenID string
	var hmacSecret []byte
	wait.Until(t, func() bool {
		sec := &corev1.Secret{}
		if err := e.cl.Get(ctx, types.NamespacedName{Name: identityName + "-access-token", Namespace: testNamespace}, sec); err != nil {
			return false
		}
		raw, ok := sec.Data[accessv1alpha1.AccessTokenSecretKey]
		if !ok || len(raw) == 0 {
			return false
		}
		tok, err := pmaccessproto.ParseToken(string(raw))
		if err != nil {
			return false
		}
		tokenID = tok.TokenID
		hmacSecret = tok.Secret
		return tokenID != ""
	}, defaultTestTimeout, defaultTickInterval, "access-token Secret should be minted")

	// 2. Serve the handler on a local httptest server backed by the
	// same workspace client the controllers use.
	srv := httptest.NewServer(&server.Handler{
		Client:      e.cl,
		Scheme:      e.cl.Scheme(),
		ReplayCache: pmaccessproto.NewReplayCache(0),
	})
	t.Cleanup(srv.Close)

	// 3. Build + sign a handshake body, then POST it.
	body, err := json.Marshal(pmaccessv1.HandshakeRequest{
		KcpURL:      "https://pm.example.test:6443/clusters/root:provider",
		KcpCABundle: "-----BEGIN CERTIFICATE-----\nMIIBdummy\n-----END CERTIFICATE-----\n",
		SAToken:     "dummy-sa-token",
	})
	require.NoError(t, err)

	jwt, err := pmaccessproto.SignCallCredential(hmacSecret, tokenID, body, 60*time.Second)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/access", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	require.Equalf(t, http.StatusOK, resp.StatusCode, "handshake failed: %s", string(respBody))

	// 4. Exactly one Access with the expected labels and owner exists.
	var accesses accessv1alpha1.AccessList
	require.NoError(t, e.cl.List(ctx, &accesses,
		ctrlruntimeclient.InNamespace(testNamespace),
		ctrlruntimeclient.MatchingLabels{server.AccessIdentityLabelKey: identityName}))
	require.Len(t, accesses.Items, 1)
	acc := accesses.Items[0]
	assert.Equal(t, identityName, acc.Spec.IdentityRef.Name)
	require.NotNil(t, acc.Spec.KubeconfigSecretRef)
	assert.Equal(t, acc.Name+"-kubeconfig", acc.Spec.KubeconfigSecretRef.Name)

	// Owner reference back to the AccessIdentity.
	ownedBy := false
	for _, ref := range acc.OwnerReferences {
		if ref.Kind == "AccessIdentity" && ref.Name == identityName {
			ownedBy = true
			break
		}
	}
	assert.True(t, ownedBy, "Access should be owned by its AccessIdentity")

	// markAccessActive ran.
	assert.Equal(t, accessv1alpha1.AccessPhaseActive, acc.Status.Phase)
	assert.NotNil(t, acc.Status.LastRefreshed)

	// 5. Kubeconfig Secret carries the handshake payload.
	kcSec := &corev1.Secret{}
	require.NoError(t, e.cl.Get(ctx, types.NamespacedName{Name: acc.Spec.KubeconfigSecretRef.Name, Namespace: testNamespace}, kcSec))
	assert.Equal(t, "true", kcSec.Labels[server.AccessOperatorLabelKey])
	assert.Contains(t, string(kcSec.Data["kubeconfig"]), "dummy-sa-token")
}
