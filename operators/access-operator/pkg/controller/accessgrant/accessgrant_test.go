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
	"testing"

	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	pmaccessproto "go.platform-mesh.io/protocol/access"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// newTestScheme returns a scheme with corev1 and the access/v1alpha1
// API group registered — enough for the fake client used in phase
// tests that need to serve/mutate Secrets and AccessGrants.
func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	sch := runtime.NewScheme()
	if err := corev1.AddToScheme(sch); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	if err := pmaccessv1alpha1.AddToScheme(sch); err != nil {
		t.Fatalf("add access/v1alpha1 to scheme: %v", err)
	}
	return sch
}

// testFixture bundles the inputs to the reconcile chain so tests can
// mutate one field (e.g. tweak Spec.AccessURL) without rebuilding the
// grant + mirror Secret from scratch.
type testFixture struct {
	token  pmaccessproto.Token
	grant  *pmaccessv1alpha1.AccessGrant
	secret *corev1.Secret
}

func newFixture(t *testing.T, accessURL string) testFixture {
	t.Helper()
	tok := pmaccessproto.Token{
		TokenID: "id-xyz",
		Secret:  []byte("hmac-secret-material"),
	}
	raw := tok.String()
	return testFixture{
		token: tok,
		grant: &pmaccessv1alpha1.AccessGrant{
			ObjectMeta: metav1.ObjectMeta{Name: "p-access-grant", Namespace: "default"},
			Spec: pmaccessv1alpha1.AccessGrantSpec{
				SARef:     corev1.LocalObjectReference{Name: "p-provider"},
				AccessURL: accessURL,
				AccessTokenSecretRef: pmaccessv1alpha1.AccessTokenSecretRef{
					Name: "p-access-token",
					Key:  "accessToken",
				},
			},
		},
		secret: &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "p-access-token", Namespace: "default"},
			Data:       map[string][]byte{"accessToken": []byte(raw)},
		},
	}
}

// testCtx returns a context with a fresh logger attached, matching
// what the top-level Reconcile sets up in production.
func testCtx(t *testing.T) context.Context {
	t.Helper()
	l, err := logger.New(logger.DefaultConfig())
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	return logger.SetLoggerInContext(context.Background(), l)
}
