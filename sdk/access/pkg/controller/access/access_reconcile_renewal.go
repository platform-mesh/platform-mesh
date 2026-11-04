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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.platform-mesh.io/golang-commons/logger"
	accessv1alpha1 "go.platform-mesh.io/sdk/access/apis/v1alpha1"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("controller", "Access").
		ChildLogger("name", req.Name).ChildLogger("namespace", req.Namespace)

	access := &accessv1alpha1.Access{}
	if err := r.Client.Get(ctx, req.NamespacedName, access); err != nil {
		return ctrl.Result{}, ctrlruntimeclient.IgnoreNotFound(err)
	}
	if access.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}
	if access.Status.Phase == accessv1alpha1.AccessPhaseRevoked {
		// Terminal state: the token is no longer renewable. Deletion
		// of the Access CR is a user action and a follow-up for the
		// SDK.
		return ctrl.Result{}, nil
	}
	if access.Spec.KubeconfigSecretRef == nil {
		return ctrl.Result{}, fmt.Errorf("Access %q has no kubeconfigSecretRef", access.Name)
	}

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{Name: access.Spec.KubeconfigSecretRef.Name, Namespace: access.Namespace}
	if err := r.Client.Get(ctx, secretKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			// The handshake creates the Secret right after the Access;
			// requeue briefly if we observe the Access first.
			log.Info().Str("secret", secretKey.Name).Msg("kubeconfig Secret not present yet, requeuing")
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get kubeconfig Secret %q: %w", secretKey.Name, err)
	}

	kubeconfig, err := loadKubeconfig(secret)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("load kubeconfig from Secret %q: %w", secretKey.Name, err)
	}
	currentToken, err := currentTokenFromKubeconfig(kubeconfig)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("read current token: %w", err)
	}
	saNamespace, saName, exp, err := parseSAToken(currentToken)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("parse current SA token: %w", err)
	}

	// If the token's exp is in the past, it is no longer renewable
	// against kcp. Mark the Access Revoked and stop reconciling.
	if !exp.IsZero() && !time.Now().Before(exp) {
		log.Info().Time("exp", exp).Msg("SA token expired, marking Access Revoked")
		access.Status.Phase = accessv1alpha1.AccessPhaseRevoked
		if err := r.Client.Status().Update(ctx, access); err != nil {
			return ctrl.Result{}, fmt.Errorf("mark Access Revoked: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// If the token has a real exp and plenty of life remains, come back later.
	if !exp.IsZero() {
		threshold := time.Duration(float64(r.ttl()) * r.renewalThresholdFraction())
		if remaining := time.Until(exp); remaining > threshold {
			log.Debug().Time("exp", exp).Dur("remaining", remaining).Dur("requeueAfter", remaining-threshold).
				Msg("SA token still fresh, requeuing")
			return ctrl.Result{RequeueAfter: remaining - threshold}, nil
		}
	}

	if exp.IsZero() {
		log.Info().Str("sa", saNamespace+"/"+saName).Msg("SA token has no exp, renewing via TokenRequest")
	} else {
		log.Info().Str("sa", saNamespace+"/"+saName).Time("exp", exp).Msg("SA token near expiry, renewing")
	}

	newToken, newExp, err := r.mintSAToken(ctx, kubeconfig, saNamespace, saName)
	if err != nil {
		log.Error().Err(err).Str("sa", saNamespace+"/"+saName).Msg("TokenRequest failed")
		return ctrl.Result{}, fmt.Errorf("mint SA token: %w", err)
	}

	if err := r.writeKubeconfigWithToken(ctx, secret, kubeconfig, newToken); err != nil {
		return ctrl.Result{}, fmt.Errorf("write renewed kubeconfig Secret %q: %w", secretKey.Name, err)
	}

	now := metav1.Now()
	access.Status.LastRefreshed = &now
	if err := r.Client.Status().Update(ctx, access); err != nil {
		return ctrl.Result{}, fmt.Errorf("update Access.status for %q: %w", access.Name, err)
	}

	threshold := time.Duration(float64(r.ttl()) * r.renewalThresholdFraction())
	log.Info().Str("sa", saNamespace+"/"+saName).Time("newExp", newExp).Msg("SA token renewed")
	return ctrl.Result{RequeueAfter: time.Until(newExp) - threshold}, nil
}

// mintSAToken calls kcp's TokenRequest subresource for the given SA,
// using the current kubeconfig for authentication. Returns the new
// bearer token and its server-provided expiration timestamp.
func (r *Reconciler) mintSAToken(ctx context.Context, kubeconfig *clientcmdapi.Config, saNamespace, saName string) (string, time.Time, error) {
	restCfg, err := clientcmd.NewDefaultClientConfig(*kubeconfig, nil).ClientConfig()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build REST config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build clientset: %w", err)
	}
	ttl := int64(r.ttl().Seconds())
	tr, err := clientset.CoreV1().ServiceAccounts(saNamespace).CreateToken(ctx, saName,
		&authv1.TokenRequest{Spec: authv1.TokenRequestSpec{ExpirationSeconds: &ttl}},
		metav1.CreateOptions{},
	)
	if err != nil {
		return "", time.Time{}, err
	}
	if tr.Status.Token == "" {
		return "", time.Time{}, fmt.Errorf("TokenRequest for %s/%s returned empty token", saNamespace, saName)
	}
	return tr.Status.Token, tr.Status.ExpirationTimestamp.Time, nil
}

// writeKubeconfigWithToken rewrites the kubeconfig in `secret` with
// `newToken` in every AuthInfo, then updates the Secret.
func (r *Reconciler) writeKubeconfigWithToken(ctx context.Context, secret *corev1.Secret, kubeconfig *clientcmdapi.Config, newToken string) error {
	for name, auth := range kubeconfig.AuthInfos {
		if auth == nil {
			continue
		}
		auth.Token = newToken
		kubeconfig.AuthInfos[name] = auth
	}
	bytes, err := clientcmd.Write(*kubeconfig)
	if err != nil {
		return fmt.Errorf("serialize kubeconfig: %w", err)
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[kubeconfigSecretKey(secret)] = bytes
	return r.Client.Update(ctx, secret)
}

// kubeconfigSecretKey picks the data key the handshake wrote under.
// The handler writes "kubeconfig" today; this falls back to it if the
// Secret has no kubeconfig-looking key.
func kubeconfigSecretKey(secret *corev1.Secret) string {
	const defaultKey = "kubeconfig"
	if _, ok := secret.Data[defaultKey]; ok {
		return defaultKey
	}
	// Pick the first key that parses as a kubeconfig.
	for k, v := range secret.Data {
		if _, err := clientcmd.Load(v); err == nil {
			return k
		}
	}
	return defaultKey
}

func loadKubeconfig(secret *corev1.Secret) (*clientcmdapi.Config, error) {
	raw, ok := secret.Data[kubeconfigSecretKey(secret)]
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("Secret has no kubeconfig data")
	}
	return clientcmd.Load(raw)
}

// currentTokenFromKubeconfig returns the bearer token from the
// kubeconfig's current-context AuthInfo. Returns an error if no token
// is present.
func currentTokenFromKubeconfig(kubeconfig *clientcmdapi.Config) (string, error) {
	ctxName := kubeconfig.CurrentContext
	kctx, ok := kubeconfig.Contexts[ctxName]
	if !ok || kctx == nil {
		return "", fmt.Errorf("current-context %q not found", ctxName)
	}
	auth, ok := kubeconfig.AuthInfos[kctx.AuthInfo]
	if !ok || auth == nil {
		return "", fmt.Errorf("authinfo %q not found", kctx.AuthInfo)
	}
	if auth.Token == "" {
		return "", fmt.Errorf("authinfo %q has no token", kctx.AuthInfo)
	}
	return auth.Token, nil
}

// parseSAToken extracts the ServiceAccount namespace, name, and
// expiry from a Kubernetes-issued JWT. Works for both projected tokens
// (modern "kubernetes.io" claims) and legacy static tokens, by reading
// the "sub" claim which is "system:serviceaccount:NS:NAME" in both.
// The exp value is the zero Time if the claim is absent (legacy
// tokens, for example).
func parseSAToken(token string) (namespace, name string, exp time.Time, err error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", "", time.Time{}, fmt.Errorf("token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("decode claims: %w", err)
	}
	var claims struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", time.Time{}, fmt.Errorf("parse claims: %w", err)
	}
	const prefix = "system:serviceaccount:"
	if !strings.HasPrefix(claims.Sub, prefix) {
		return "", "", time.Time{}, fmt.Errorf("sub %q is not a ServiceAccount", claims.Sub)
	}
	rest := strings.TrimPrefix(claims.Sub, prefix)
	nsName := strings.SplitN(rest, ":", 2)
	if len(nsName) != 2 || nsName[0] == "" || nsName[1] == "" {
		return "", "", time.Time{}, fmt.Errorf("sub %q does not encode ns:name", claims.Sub)
	}
	if claims.Exp != 0 {
		exp = time.Unix(claims.Exp, 0)
	}
	return nsName[0], nsName[1], exp, nil
}
