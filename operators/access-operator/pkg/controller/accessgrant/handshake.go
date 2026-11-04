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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	pmaccessproto "go.platform-mesh.io/protocol/access"
)

// saTokenSecretName derives the Secret name access-operator uses to
// hold an SA-token Secret for the named ServiceAccount.
func saTokenSecretName(saName string) string {
	return saName + "-access-operator-token"
}

// defaultSATokenMinter ensures a kubernetes.io/service-account-token
// Secret exists for the SA in the Provider Workspace and returns its
// populated token. If the token field is not yet populated by the k8s
// SA-token controller it returns ("", nil) so the caller can requeue
// instead of blocking.
//
// TokenRequest would be cleaner but requires kcp to support
// subresource PermissionClaims — see RFC 012. Until a tagged kcp
// release ships that, long-lived SA-token Secrets are the only path.
func defaultSATokenMinter(ctx context.Context, cl ctrlruntimeclient.Client, saName, saNamespace string) (string, error) {
	name := saTokenSecretName(saName)
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: saNamespace,
			Annotations: map[string]string{
				corev1.ServiceAccountNameKey: saName,
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	if err := cl.Create(ctx, sec); err != nil && !apierrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("create SA-token Secret %s/%s: %w", saNamespace, name, err)
	}
	fresh := &corev1.Secret{}
	if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: saNamespace}, fresh); err != nil {
		return "", fmt.Errorf("get SA-token Secret %s/%s: %w", saNamespace, name, err)
	}
	tok := fresh.Data[corev1.ServiceAccountTokenKey]
	if len(tok) == 0 {
		return "", nil
	}
	return string(tok), nil
}

// httpClientFor returns an http.Client that verifies the /access server
// certificate against caBundlePEM if non-empty, otherwise falls back to
// base. base must be non-nil.
func httpClientFor(base *http.Client, caBundlePEM string) (*http.Client, error) {
	if caBundlePEM == "" {
		return base, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caBundlePEM)) {
		return nil, fmt.Errorf("spec.caBundle: no valid certificates parsed")
	}
	// Clone transport to avoid mutating the shared one; fall back to a
	// fresh Transport{} if base has no custom transport.
	var baseTransport *http.Transport
	if rt, ok := base.Transport.(*http.Transport); ok && rt != nil {
		baseTransport = rt.Clone()
	} else {
		baseTransport = &http.Transport{}
	}
	baseTransport.TLSClientConfig = &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}
	return &http.Client{
		Timeout:   base.Timeout,
		Transport: baseTransport,
	}, nil
}

// negotiateVersion probes <baseURL>/versions, picks the highest
// version this operator supports that the server also lists, and
// returns both the picked version and the server's full advertised
// list (the latter lets callers surface it in status when there is
// no overlap).
//
// The server's /versions endpoint is intentionally unauthenticated
// and returns a stable JSON shape; see protocol/access.
func negotiateVersion(ctx context.Context, cl *http.Client, baseURL string) (picked string, serverVersions []string, err error) {
	probeURL, err := url.JoinPath(baseURL, pmaccessproto.VersionsPath)
	if err != nil {
		return "", nil, fmt.Errorf("build /versions URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("build /versions request: %w", err)
	}
	resp, err := cl.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("GET %s: %w", probeURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", nil, fmt.Errorf("GET %s: HTTP %d: %s", probeURL, resp.StatusCode, trim(string(body)))
	}
	var payload pmaccessproto.VersionsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload); err != nil {
		return "", nil, fmt.Errorf("decode /versions response: %w", err)
	}
	// Operator preference order is pmaccessproto.SupportedVersions
	// (newest first). Pick the first one the server also offers.
	serverSet := make(map[string]struct{}, len(payload.Versions))
	for _, v := range payload.Versions {
		serverSet[v] = struct{}{}
	}
	for _, v := range pmaccessproto.SupportedVersions {
		if _, ok := serverSet[v]; ok {
			return v, payload.Versions, nil
		}
	}
	return "", payload.Versions, nil
}

// trim truncates s for inclusion in a condition message.
func trim(s string) string {
	const max = 200
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
