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
	"testing"

	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidate_ValidToken_SetsAccepted(t *testing.T) {
	fx := newFixture(t, "https://provider.example")
	cl := fake.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(fx.grant, fx.secret).Build()

	v := &validateReconciler{}
	parsed, _, done, err := v.reconcile(context.Background(), cl, fx.grant)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if done {
		t.Fatalf("expected done=false on valid token")
	}
	if parsed.TokenID != fx.token.TokenID || !bytes.Equal(parsed.Secret, fx.token.Secret) {
		t.Errorf("parsed token = %+v, want %+v", parsed, fx.token)
	}
	cond := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionValid)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != pmaccessv1alpha1.AccessGrantAcceptedReason {
		t.Errorf("Valid condition = %+v, want True/Accepted", cond)
	}
}

func TestValidate_SecretMissing_SetsReasonAndStops(t *testing.T) {
	fx := newFixture(t, "https://provider.example")
	// Seed only the grant, not the Secret.
	cl := fake.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(fx.grant).Build()

	v := &validateReconciler{}
	_, res, done, err := v.reconcile(context.Background(), cl, fx.grant)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !done || res.RequeueAfter == 0 {
		t.Errorf("expected done=true with RequeueAfter > 0, got done=%v res=%+v", done, res)
	}
	cond := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionValid)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != pmaccessv1alpha1.AccessGrantAccessTokenSecretMissingReason {
		t.Errorf("Valid condition = %+v, want False/AccessTokenSecretMissing", cond)
	}
}

func TestValidate_EmptyKey_SetsReasonAndStops(t *testing.T) {
	fx := newFixture(t, "https://provider.example")
	fx.secret.Data = map[string][]byte{"accessToken": nil}
	cl := fake.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(fx.grant, fx.secret).Build()

	v := &validateReconciler{}
	_, _, done, err := v.reconcile(context.Background(), cl, fx.grant)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !done {
		t.Fatalf("expected done=true on empty token key")
	}
	cond := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionValid)
	if cond == nil || cond.Reason != pmaccessv1alpha1.AccessGrantAccessTokenSecretMissingReason {
		t.Errorf("Valid condition = %+v, want AccessTokenSecretMissing", cond)
	}
}

func TestValidate_InvalidToken_SetsReasonAndStops(t *testing.T) {
	fx := newFixture(t, "https://provider.example")
	fx.secret.Data["accessToken"] = []byte("not-a-pma1-token")
	cl := fake.NewClientBuilder().WithScheme(newTestScheme(t)).WithObjects(fx.grant, fx.secret).Build()

	v := &validateReconciler{}
	_, _, done, err := v.reconcile(context.Background(), cl, fx.grant)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !done {
		t.Fatalf("expected done=true on invalid token")
	}
	cond := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionValid)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != pmaccessv1alpha1.AccessGrantInvalidAccessTokenReason {
		t.Errorf("Valid condition = %+v, want False/InvalidAccessToken", cond)
	}
	if c := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted); c != nil {
		t.Errorf("Connected condition must be cleared on invalid token, got %+v", c)
	}
}
