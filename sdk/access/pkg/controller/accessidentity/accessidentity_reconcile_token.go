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

package accessidentity

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"

	"go.platform-mesh.io/golang-commons/logger"
	pmaccessproto "go.platform-mesh.io/protocol/access"
	accessv1alpha1 "go.platform-mesh.io/sdk/access/apis/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func accessTokenSecretName(identity *accessv1alpha1.AccessIdentity) string {
	return identity.Name + "-access-token"
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("controller", "AccessIdentity").
		ChildLogger("name", req.Name).ChildLogger("namespace", req.Namespace)

	identity := &accessv1alpha1.AccessIdentity{}
	if err := r.Client.Get(ctx, req.NamespacedName, identity); err != nil {
		return ctrl.Result{}, ctrlruntimeclient.IgnoreNotFound(err)
	}

	secretName := accessTokenSecretName(identity)
	secret := &corev1.Secret{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: identity.Namespace}, secret)

	switch {
	case apierrors.IsNotFound(err):
		// Secret is missing: either this is a new AccessIdentity or it was deleted
		// (i.e. user wants rotation). We create it now.
		log.Info().Str("secret", secretName).Msg("Secret missing, minting access token")
		tokenID, err := r.mint(ctx, identity, secretName)
		if err != nil {
			log.Error().Err(err).Str("secret", secretName).Msg("mint failed")
			return ctrl.Result{}, err
		}
		identity.Status.TokenID = tokenID
		setAccessIdentityReady(identity, metav1.ConditionTrue, accessv1alpha1.AccessIdentityReadyReason,
			"access token is ready")
		log.Info().Str("tokenID", tokenID).Msg("access token minted")
		return ctrl.Result{}, r.Client.Status().Update(ctx, identity)

	case err != nil:
		return ctrl.Result{}, fmt.Errorf("get Secret %q: %w", secretName, err)
	}

	// Secret exists. Ensure status.TokenID matches the ID on the Secret.
	tokenID, err := tokenIDFromSecret(secret)
	if err != nil {
		log.Info().Err(err).Str("secret", secretName).Msg("Secret present but not usable")
		setAccessIdentityReady(identity, metav1.ConditionFalse, accessv1alpha1.AccessIdentityNotReadyAwaitingSecretReason, err.Error())
		return ctrl.Result{}, r.Client.Status().Update(ctx, identity)
	}
	if identity.Status.TokenID != tokenID {
		log.Info().Str("tokenID", tokenID).Msg("status.tokenID reconciled from Secret")
		identity.Status.TokenID = tokenID
		setAccessIdentityReady(identity, metav1.ConditionTrue, accessv1alpha1.AccessIdentityReadyReason,
			"access token is ready")
		return ctrl.Result{}, r.Client.Status().Update(ctx, identity)
	}

	return ctrl.Result{}, nil
}

// tokenIDFromSecret returns the tokenID carried by the Secret,
// preferring the TokenIDLabelKey label (authoritative, set atomically
// by mint) and falling back to parsing the access-token data for
// Secrets written before the label was introduced.
func tokenIDFromSecret(secret *corev1.Secret) (string, error) {
	if id, ok := secret.Labels[accessv1alpha1.TokenIDLabelKey]; ok && id != "" {
		return id, nil
	}
	rawToken, ok := secret.Data[accessv1alpha1.AccessTokenSecretKey]
	if !ok || len(rawToken) == 0 {
		return "", fmt.Errorf("access token is missing")
	}
	parsedToken, err := pmaccessproto.ParseToken(string(rawToken))
	if err != nil {
		return "", fmt.Errorf("access token did not parse: %w", err)
	}
	return parsedToken.TokenID, nil
}

// mint generates a fresh (tokenID, hmacSecret) and writes it to Secret using Create.
// Returns tokenID on success.
func (r *Reconciler) mint(ctx context.Context, identity *accessv1alpha1.AccessIdentity, secretName string) (string, error) {
	tokenID, err := newTokenID()
	if err != nil {
		return "", fmt.Errorf("generate tokenID: %w", err)
	}
	hmacSecret, err := newHMACSecret()
	if err != nil {
		return "", fmt.Errorf("generate HMAC secret: %w", err)
	}

	accessToken := pmaccessproto.Token{
		TokenID: tokenID,
		Secret:  hmacSecret,
	}.String()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: identity.Namespace,
			Labels:    map[string]string{accessv1alpha1.TokenIDLabelKey: tokenID},
		},
		Data: map[string][]byte{accessv1alpha1.AccessTokenSecretKey: []byte(accessToken)},
	}
	if err := controllerutil.SetControllerReference(identity, secret, r.Scheme); err != nil {
		return "", fmt.Errorf("set owner reference on Secret %q: %w", secretName, err)
	}
	if err := r.Client.Create(ctx, secret); err != nil {
		return "", fmt.Errorf("create Secret %q: %w", secretName, err)
	}

	return tokenID, nil
}

func setAccessIdentityReady(identity *accessv1alpha1.AccessIdentity, status metav1.ConditionStatus, reason, msg string) {
	apimeta.SetStatusCondition(&identity.Status.Conditions, metav1.Condition{
		Type:               accessv1alpha1.AccessIdentityReady,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: identity.Generation,
	})
}

// newTokenID returns a random tokenID. Not sensitive.
func newTokenID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf[:])), nil
}

// newHMACSecret returns 32 random bytes for use
// as the HMAC key in the per-/access call credential.
// This is the sensitive part of the token.
func newHMACSecret() ([]byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}
