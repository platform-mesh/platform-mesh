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

// Package server implements the user-side /access HTTP handlers.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	pmaccessproto "go.platform-mesh.io/protocol/access"
	pmaccessv1 "go.platform-mesh.io/protocol/access/v1"
	accessv1alpha1 "go.platform-mesh.io/sdk/access/apis/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const kubeconfigSecretKey = "kubeconfig"

// AccessOperatorLabelKey is the label key applied on every kubeconfig Secret we write.
// Clients can filter by this. Value is set to just "true" - not used.
const AccessOperatorLabelKey = "access-sdk.platform-mesh.io/kubeconfig"

// AccessIdentityLabelKey is the label key we write on every Access.
// Its value is the name of this Access's parent AccessIdentity.
const AccessIdentityLabelKey = "access-sdk.platform-mesh.io/identity"

// maxRequestBodyBytes caps the handshake request body.
//
// The actual payload carries only the URL, a PEM CA bundle, and a k8s SA token.
const maxRequestBodyBytes = 64 << 10

// Handler serves POST /v1/access on the user-side cluster. The caller
// (a PM access-operator) presents a Bearer JWT signed with the HMAC
// key of some AccessIdentity on this cluster, plus a body containing
// the kubeconfig to deliver. On a verified handshake the Handler
// creates an Access CR and writes the kubeconfig into a Secret owned
// by that Access, then responds 200 with an empty body.
//
// This type is a plain http.Handler. TLS termination, listen address,
// timeouts, and connection caps live in cmd/access-server.
type Handler struct {
	Client      client.Client
	Scheme      *runtime.Scheme
	ReplayCache *pmaccessproto.ReplayCache
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	jwt, err := parseBearer(r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, logFromRequest(r), err)
		return
	}
	handshake, err := h.doHandshake(r.Context(), jwt, body)
	if err != nil {
		writeError(w, logFromRequest(r), err)
		return
	}
	if err := h.doCommit(r.Context(), handshake); err != nil {
		writeError(w, logFromRequest(r), err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type verifiedCredential struct {
	identity *accessv1alpha1.AccessIdentity
	jti      string
}

type validatedHandshake struct {
	identity *accessv1alpha1.AccessIdentity
	payload  *pmaccessv1.HandshakeRequest
	jti      string
}

func (h *Handler) doHandshake(ctx context.Context, jwt string, body []byte) (*validatedHandshake, error) {
	cred, err := h.verifyCredential(ctx, jwt, body)
	if err != nil {
		return nil, err
	}
	payload, err := decodePayload(body)
	if err != nil {
		return nil, err
	}
	return &validatedHandshake{identity: cred.identity, payload: payload, jti: cred.jti}, nil
}

func (h *Handler) verifyCredential(ctx context.Context, jwt string, body []byte) (*verifiedCredential, error) {
	// Read sub from the token to resolve which Secret this handshake is for.
	// We don't trust it yet, so all the code below, up until Verify(), treats it as such.
	pre, err := pmaccessproto.ParseUnverified(jwt)
	if err != nil {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, fmt.Errorf("parse credential: %w", err))
	}

	secret, err := h.findSecretByTokenID(ctx, pre.Subject)
	if err != nil {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, err)
	}

	hmacSecret, err := extractHMACSecret(secret)
	if err != nil {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, err)
	}

	claims, err := pmaccessproto.Verify(hmacSecret, jwt, body, 0)
	if err != nil {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, fmt.Errorf("verify: %w", err))
	}
	if h.ReplayCache != nil && !h.ReplayCache.AdmitIfNew(claims.Subject, claims.ID, claims.ExpiresAt.Time) {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, fmt.Errorf("replay for sub=%s jti=%s", claims.Subject, claims.ID))
	}

	identity, err := h.getAccessIdentityBySecret(ctx, secret)
	if err != nil {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, err)
	}

	return &verifiedCredential{identity: identity, jti: claims.ID}, nil
}

func (h *Handler) doCommit(ctx context.Context, handshake *validatedHandshake) error {
	accessName := buildAccessName(handshake.identity.Name, handshake.jti)
	kubeconfigSecretName := accessName + "-kubeconfig"

	access, err := h.createAccess(ctx, handshake.identity, accessName, kubeconfigSecretName)
	if err != nil {
		return httpErr(http.StatusInternalServerError, publicInternalError, err)
	}
	if err := h.writeKubeconfigSecret(ctx, access, kubeconfigSecretName, handshake.identity.Namespace, handshake.payload); err != nil {
		return httpErr(http.StatusInternalServerError, publicInternalError, err)
	}
	if err := h.markAccessActive(ctx, access); err != nil {
		return httpErr(http.StatusInternalServerError, publicInternalError, err)
	}
	return nil
}

// buildAccessName derives a deterministic Access CR name from an
// identity name and a jti. Collisions on different jtis are bounded
// by a 40-bit SHA-256 prefix (~2^-40).
func buildAccessName(identityName, jti string) string {
	sum := sha256.Sum256([]byte(jti))
	return identityName + "-" + hex.EncodeToString(sum[:5])
}

func parseBearer(auth string) (string, error) {
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || auth[:len(prefix)] != prefix {
		return "", httpErr(http.StatusForbidden, publicForbidden, fmt.Errorf("missing Bearer credential"))
	}
	return auth[len(prefix):], nil
}

func decodePayload(body []byte) (*pmaccessv1.HandshakeRequest, error) {
	var payload pmaccessv1.HandshakeRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, fmt.Errorf("parse body: %w", err))
	}
	if payload.KcpURL == "" || payload.SAToken == "" {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, fmt.Errorf("body missing kcpURL or saToken"))
	}
	u, err := url.Parse(payload.KcpURL)
	if err != nil {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, fmt.Errorf("parse kcpURL: %w", err))
	}
	if u.Scheme != "https" {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, fmt.Errorf("kcpURL scheme must be https, got %q", u.Scheme))
	}
	if u.Host == "" {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, fmt.Errorf("kcpURL missing host"))
	}
	if !strings.HasPrefix(u.Path, "/clusters/") {
		return nil, httpErr(http.StatusForbidden, publicInvalidCredential, fmt.Errorf("kcpURL missing cluster name"))
	}
	return &payload, nil
}

// findSecretByTokenID lists Secrets labelled with the given tokenID.
// Exactly one match is expected (the controller writes a label-unique
// Secret per AccessIdentity); zero means unknown, more than one means
// an inconsistent cluster.
func (h *Handler) findSecretByTokenID(ctx context.Context, tokenID string) (*corev1.Secret, error) {
	var list corev1.SecretList
	if err := h.Client.List(ctx, &list, client.MatchingLabels{accessv1alpha1.TokenIDLabelKey: tokenID}); err != nil {
		return nil, fmt.Errorf("lookup Secret by tokenID label: %w", err)
	}
	switch len(list.Items) {
	case 0:
		return nil, fmt.Errorf("UnknownIdentity")
	case 1:
		return &list.Items[0], nil
	default:
		return nil, fmt.Errorf("AmbiguousIdentity")
	}
}

func (h *Handler) getAccessIdentityBySecret(ctx context.Context, secret *corev1.Secret) (*accessv1alpha1.AccessIdentity, error) {
	ownerRef := metav1.GetControllerOf(secret)
	if ownerRef == nil || ownerRef.Kind != "AccessIdentity" {
		return nil, fmt.Errorf("Secret %q has no AccessIdentity controller owner", secret.Name)
	}
	identity := &accessv1alpha1.AccessIdentity{}
	if err := h.Client.Get(ctx, types.NamespacedName{Name: ownerRef.Name, Namespace: secret.Namespace}, identity); err != nil {
		return nil, fmt.Errorf("get AccessIdentity %q: %w", ownerRef.Name, err)
	}
	return identity, nil
}

// extractHMACSecret parses the access token from the Secret's data
// and returns the HMAC key bytes.
func extractHMACSecret(secret *corev1.Secret) ([]byte, error) {
	rawToken, ok := secret.Data[accessv1alpha1.AccessTokenSecretKey]
	if !ok || len(rawToken) == 0 {
		return nil, fmt.Errorf("Secret %q has no access-token value", secret.Name)
	}
	parsedToken, err := pmaccessproto.ParseToken(string(rawToken))
	if err != nil {
		return nil, fmt.Errorf("Secret %q does not parse: %w", secret.Name, err)
	}
	return parsedToken.Secret, nil
}

func (h *Handler) createAccess(ctx context.Context, identity *accessv1alpha1.AccessIdentity, accessName, kubeconfigSecretName string) (*accessv1alpha1.Access, error) {
	access := &accessv1alpha1.Access{
		ObjectMeta: metav1.ObjectMeta{
			Name:      accessName,
			Namespace: identity.Namespace,
			Labels:    map[string]string{AccessIdentityLabelKey: identity.Name},
		},
		Spec: accessv1alpha1.AccessSpec{
			IdentityRef:         corev1.LocalObjectReference{Name: identity.Name},
			KubeconfigSecretRef: &corev1.LocalObjectReference{Name: kubeconfigSecretName},
		},
	}
	if err := controllerutil.SetOwnerReference(identity, access, h.Scheme); err != nil {
		return nil, fmt.Errorf("set owner reference on Access %q: %w", accessName, err)
	}
	if err := h.Client.Create(ctx, access); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create Access %q: %w", accessName, err)
	}
	return access, nil
}

func (h *Handler) writeKubeconfigSecret(ctx context.Context, access *accessv1alpha1.Access, name, namespace string, payload *pmaccessv1.HandshakeRequest) error {
	kubeconfigBytes, err := buildKubeconfig(payload)
	if err != nil {
		return fmt.Errorf("build kubeconfig: %w", err)
	}

	kcSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, h.Client, kcSecret, func() error {
		if err := controllerutil.SetControllerReference(access, kcSecret, h.Scheme); err != nil {
			return err
		}
		if kcSecret.Labels == nil {
			kcSecret.Labels = map[string]string{}
		}
		kcSecret.Labels[AccessOperatorLabelKey] = "true"
		if kcSecret.Data == nil {
			kcSecret.Data = map[string][]byte{}
		}
		kcSecret.Data[kubeconfigSecretKey] = kubeconfigBytes
		return nil
	}); err != nil {
		return fmt.Errorf("upsert kubeconfig Secret %q: %w", name, err)
	}
	return nil
}

func (h *Handler) markAccessActive(ctx context.Context, access *accessv1alpha1.Access) error {
	access.Status.Phase = accessv1alpha1.AccessPhaseActive
	now := metav1.Now()
	access.Status.LastRefreshed = &now
	if err := h.Client.Status().Update(ctx, access); err != nil {
		return fmt.Errorf("update Access.status for %q: %w", access.Name, err)
	}
	return nil
}

func buildKubeconfig(payload *pmaccessv1.HandshakeRequest) ([]byte, error) {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters = map[string]*clientcmdapi.Cluster{
		"pm-kcp": {
			Server:                   payload.KcpURL,
			CertificateAuthorityData: []byte(payload.KcpCABundle),
		},
	}
	cfg.AuthInfos = map[string]*clientcmdapi.AuthInfo{
		"pm-sa": {Token: payload.SAToken},
	}
	cfg.Contexts = map[string]*clientcmdapi.Context{
		"pm": {Cluster: "pm-kcp", AuthInfo: "pm-sa"},
	}
	cfg.CurrentContext = "pm"
	return clientcmd.Write(*cfg)
}
