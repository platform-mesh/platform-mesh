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
	"fmt"

	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// saTokenReconciler ensures an SA-token Secret exists for the
// Provider Workspace's ServiceAccount and reads back its populated
// bearer token.
type saTokenReconciler struct {
	mintTokenForServiceAccount func(ctx context.Context, cl ctrlruntimeclient.Client, saName, saNamespace string) (string, error)
}

// reconcile returns the SA bearer token on success. When the k8s
// SA-token controller has not yet populated the Secret the minter
// returns ("", nil) and this phase sets Connected=False/SATokenPending
// with a short requeue.
func (s *saTokenReconciler) reconcile(ctx context.Context, cl ctrlruntimeclient.Client, grant *pmaccessv1alpha1.AccessGrant) (string, ctrl.Result, bool, error) {
	saToken, err := s.mintTokenForServiceAccount(ctx, cl, grant.Spec.SARef.Name, grant.Namespace)
	if err != nil {
		return "", ctrl.Result{}, true, fmt.Errorf("mint SA token for %s/%s: %w", grant.Namespace, grant.Spec.SARef.Name, err)
	}
	if saToken == "" {
		setConnected(grant, metav1.ConditionFalse,
			pmaccessv1alpha1.AccessGrantSATokenPendingReason,
			"waiting for k8s SA-token controller to populate the SA-token Secret")
		return "", ctrl.Result{RequeueAfter: retrySATokenEmpty}, true, nil
	}
	return saToken, ctrl.Result{}, false, nil
}
