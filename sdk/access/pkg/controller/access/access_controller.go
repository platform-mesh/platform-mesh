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

// Package access hosts the Access controller. It renews the
// ServiceAccount token embedded in each Access's kubeconfig Secret by
// calling TokenRequest against kcp using the current kubeconfig, and
// updates the Secret with the fresh token before the current one
// expires.
package access

import (
	"time"

	accessv1alpha1 "go.platform-mesh.io/sdk/access/apis/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Defaults for the token-renewal reconciler.
const (
	// DefaultAccessTokenTTL is the lifetime requested from kcp's
	// TokenRequest API for each renewed ServiceAccount token.
	DefaultAccessTokenTTL = 1 * time.Hour

	// DefaultRenewalThresholdFraction is the fraction of the current
	// token's remaining lifetime below which the reconciler renews.
	// With 0.2, the reconciler renews when less than 20% of the TTL
	// is left.
	DefaultRenewalThresholdFraction = 0.2
)

// Reconciler watches Access objects and renews the ServiceAccount
// token stored in each Access's kubeconfig Secret before it expires.
// The current kubeconfig is used to call TokenRequest against kcp; the
// renewed token is written back into the same Secret.
type Reconciler struct {
	Client ctrlruntimeclient.Client
	Scheme *runtime.Scheme

	// TTL requested from TokenRequest. Zero means DefaultAccessTokenTTL.
	TTL time.Duration

	// RenewalThresholdFraction below which a token is renewed. Zero
	// means DefaultRenewalThresholdFraction.
	RenewalThresholdFraction float64
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("AccessReconciler").
		For(&accessv1alpha1.Access{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func (r *Reconciler) ttl() time.Duration {
	if r.TTL <= 0 {
		return DefaultAccessTokenTTL
	}
	return r.TTL
}

func (r *Reconciler) renewalThresholdFraction() float64 {
	if r.RenewalThresholdFraction <= 0 {
		return DefaultRenewalThresholdFraction
	}
	return r.RenewalThresholdFraction
}
