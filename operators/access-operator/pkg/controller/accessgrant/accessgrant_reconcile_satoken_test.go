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
	"errors"
	"testing"

	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSAToken_Success(t *testing.T) {
	fx := newFixture(t, "https://provider.example")
	s := &saTokenReconciler{
		mintTokenForServiceAccount: func(context.Context, ctrlruntimeclient.Client, string, string) (string, error) {
			return "stub-sa-token", nil
		},
	}
	tok, _, done, err := s.reconcile(context.Background(), nil, fx.grant)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if done {
		t.Fatalf("expected done=false on success")
	}
	if tok != "stub-sa-token" {
		t.Errorf("tok = %q, want stub-sa-token", tok)
	}
}

func TestSAToken_EmptyToken_SetsPending(t *testing.T) {
	fx := newFixture(t, "https://provider.example")
	s := &saTokenReconciler{
		mintTokenForServiceAccount: func(context.Context, ctrlruntimeclient.Client, string, string) (string, error) {
			return "", nil
		},
	}
	tok, res, done, err := s.reconcile(context.Background(), nil, fx.grant)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if tok != "" {
		t.Errorf("tok = %q, want empty", tok)
	}
	if !done || res.RequeueAfter != retrySATokenEmpty {
		t.Errorf("expected done=true with RequeueAfter=%v, got done=%v res=%+v", retrySATokenEmpty, done, res)
	}
	cond := apimeta.FindStatusCondition(fx.grant.Status.Conditions, pmaccessv1alpha1.AccessGrantConditionHandshakeCompleted)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != pmaccessv1alpha1.AccessGrantSATokenPendingReason {
		t.Errorf("Connected condition = %+v, want False/SATokenPending", cond)
	}
}

func TestSAToken_MinterError_PropagatesError(t *testing.T) {
	fx := newFixture(t, "https://provider.example")
	wantErr := errors.New("boom")
	s := &saTokenReconciler{
		mintTokenForServiceAccount: func(context.Context, ctrlruntimeclient.Client, string, string) (string, error) {
			return "", wantErr
		},
	}
	_, _, done, err := s.reconcile(context.Background(), nil, fx.grant)
	if !done {
		t.Errorf("expected done=true when minter errors")
	}
	if err == nil || !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wrapping %v", err, wantErr)
	}
}
