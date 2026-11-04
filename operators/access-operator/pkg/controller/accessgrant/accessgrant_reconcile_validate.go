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
	pmaccessproto "go.platform-mesh.io/protocol/access"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// validateReconciler reads the access-token Secret referenced by the
// grant and parses the pma1 access token.
type validateReconciler struct{}

// reconcile validates the AccessGrant by parsing the referenced access token.
// If valid, it sets AccessGrantConditionValid=True, and also returns that parsed token.
func (*validateReconciler) reconcile(ctx context.Context, cl ctrlruntimeclient.Client, grant *pmaccessv1alpha1.AccessGrant) (pmaccessproto.Token, ctrl.Result, bool, error) {
	ref := grant.Spec.AccessTokenSecretRef
	key := ref.Key
	if key == "" {
		key = "accessToken"
	}
	tokenSecret := &corev1.Secret{}
	if err := cl.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: grant.Namespace}, tokenSecret); err != nil {
		if apierrors.IsNotFound(err) {
			setValid(grant, metav1.ConditionFalse,
				pmaccessv1alpha1.AccessGrantAccessTokenSecretMissingReason,
				fmt.Sprintf("Secret %s/%s not found", grant.Namespace, ref.Name))
			clearConnected(grant)
			return pmaccessproto.Token{}, ctrl.Result{RequeueAfter: retryRejected}, true, nil
		}
		return pmaccessproto.Token{}, ctrl.Result{}, true, fmt.Errorf("get access-token Secret %s/%s: %w", grant.Namespace, ref.Name, err)
	}
	rawToken, ok := tokenSecret.Data[key]
	if !ok || len(rawToken) == 0 {
		setValid(grant, metav1.ConditionFalse,
			pmaccessv1alpha1.AccessGrantAccessTokenSecretMissingReason,
			fmt.Sprintf("Secret %s/%s has no value at key %q", grant.Namespace, ref.Name, key))
		clearConnected(grant)
		return pmaccessproto.Token{}, ctrl.Result{RequeueAfter: retryRejected}, true, nil
	}

	parsed, err := pmaccessproto.ParseToken(string(rawToken))
	if err != nil {
		setValid(grant, metav1.ConditionFalse,
			pmaccessv1alpha1.AccessGrantInvalidAccessTokenReason,
			fmt.Sprintf("access token did not parse: %v", err))
		clearConnected(grant)
		return pmaccessproto.Token{}, ctrl.Result{RequeueAfter: retryRejected}, true, nil
	}

	setValid(grant, metav1.ConditionTrue,
		pmaccessv1alpha1.AccessGrantAcceptedReason,
		"AccessGrant is valid")
	return parsed, ctrl.Result{}, false, nil
}
