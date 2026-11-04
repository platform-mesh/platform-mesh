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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	pmaccessproto "go.platform-mesh.io/protocol/access"
	pmaccessv1 "go.platform-mesh.io/protocol/access/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// handshakeReconciler builds the HandshakeRequest body, signs the JWT
// call credential, and POSTs /access at the negotiated version.
//
// invalidateVer is called on dial failure so a stale cached version
// (e.g. one the provider dropped after an SDK upgrade) is re-probed
// on the next retry.
type handshakeReconciler struct {
	kcpExternalURL string
	readCABundle   func() ([]byte, error)
	invalidateVer  func(baseURL string)
}

// reconcile maps the POST response to a Connected condition:
// 200 → True/HandshakeSucceeded, 202 → False/HandshakePending,
// 403 → False/HandshakeRejected, other / dial error →
// False/AccessURLUnreachable.
func (h *handshakeReconciler) reconcile(
	ctx context.Context,
	httpClient *http.Client,
	grant *pmaccessv1alpha1.AccessGrant,
	clusterName string,
	version string,
	parsedAccessToken pmaccessproto.Token,
	saToken string,
) (ctrl.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)
	caBundle, err := h.readCABundle()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("read kcp CA bundle: %w", err)
	}

	kcpURL, err := url.JoinPath(h.kcpExternalURL, "clusters", clusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build kcp workspace URL: %w", err)
	}
	body := pmaccessv1.HandshakeRequest{
		KcpURL:      kcpURL,
		KcpCABundle: string(caBundle),
		SAToken:     saToken,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("marshal /access body: %w", err)
	}

	jwt, err := pmaccessproto.SignCallCredential(parsedAccessToken.Secret, parsedAccessToken.TokenID, bodyBytes, 0)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("sign JWT call credential: %w", err)
	}

	accessURL, err := url.JoinPath(grant.Spec.AccessURL, pmaccessproto.AccessPath(version))
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build /access URL: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, accessURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build /access request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+jwt)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		log.Info().Err(err).Str("url", accessURL).Msg("/access dial failed")
		h.invalidateVer(grant.Spec.AccessURL)
		setConnected(grant, metav1.ConditionFalse,
			pmaccessv1alpha1.AccessGrantAccessURLUnreachableReason,
			fmt.Sprintf("dial %s: %v", accessURL, err))
		return ctrl.Result{RequeueAfter: retryUnreachable}, nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	switch resp.StatusCode {
	case http.StatusOK:
		setConnected(grant, metav1.ConditionTrue,
			pmaccessv1alpha1.AccessGrantHandshakeSucceededReason,
			"handshake completed")
		log.Info().Str("url", accessURL).Msg("handshake succeeded")
		return ctrl.Result{}, nil
	default:
		setConnected(grant, metav1.ConditionFalse,
			pmaccessv1alpha1.AccessGrantAccessURLUnreachableReason,
			fmt.Sprintf("unexpected %d from /access: %s", resp.StatusCode, trim(string(respBody))))
		return ctrl.Result{RequeueAfter: retryOther}, nil
	}
}
