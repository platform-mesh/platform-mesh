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

// Package accessidentity hosts the AccessIdentity controller. It mints
// an access token (tokenID + HMAC secret) per AccessIdentity and
// writes it into a labelled Secret. Deleting that Secret is how
// rotation is triggered — the next reconcile mints a fresh pair.
package accessidentity

import (
	accessv1alpha1 "go.platform-mesh.io/sdk/access/apis/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Reconciler mints an access token (tokenID + HMAC secret) and writes
// it into a Secret, labelled with the current tokenID. The Secret is
// the sole source of truth for the credential: deleting it is how
// rotation is triggered — the next reconcile mints a fresh
// (tokenID, HMAC) and writes a new Secret. Existing Accesses are
// unaffected; handshakes under the old token fail when the sub is
// being looked up.
type Reconciler struct {
	Client ctrlruntimeclient.Client
	Scheme *runtime.Scheme
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("AccessIdentityReconciler").
		For(&accessv1alpha1.AccessIdentity{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
