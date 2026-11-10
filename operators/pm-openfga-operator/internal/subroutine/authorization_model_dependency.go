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

package subroutine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// authorizationModelFinalizerPrefix marks the finalizers a Store carries for
// each AuthorizationModel that depends on it.
const authorizationModelFinalizerPrefix = "authzmodel.core.platform-mesh.io/"

// authorizationModelFinalizer returns the finalizer name for an
// AuthorizationModel's dependency on its Store. Hashed because finalizer
// names are capped at 63 bytes, which raw model names can exceed.
func authorizationModelFinalizer(modelCluster, modelName string) string {
	sum := sha256.Sum256([]byte(modelCluster + "/" + modelName))
	// 31 bytes (62 hex chars) stays under the 63-byte cap.
	return authorizationModelFinalizerPrefix + hex.EncodeToString(sum[:31])
}

// registerAuthorizationModelWithStore adds a finalizer marking that the
// given AuthorizationModel depends on storeName. The API server rejects
// this once the Store has a deletionTimestamp, so callers must not create
// the AuthorizationModel unless this succeeds.
//
// storeCluster must already be formatted the way the caller's manager
// expects.
func registerAuthorizationModelWithStore(ctx context.Context, mgr mcmanager.Manager, storeCluster multicluster.ClusterName, storeName, modelCluster, modelName string) error {
	cl, err := mgr.GetCluster(ctx, storeCluster)
	if err != nil {
		return fmt.Errorf("getting store cluster: %w", err)
	}
	finalizer := authorizationModelFinalizer(modelCluster, modelName)

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var store pmcorev1alpha1.Store
		if err := cl.GetClient().Get(ctx, types.NamespacedName{Name: storeName}, &store); err != nil {
			return err
		}
		if controllerutil.ContainsFinalizer(&store, finalizer) {
			return nil
		}
		controllerutil.AddFinalizer(&store, finalizer)
		return cl.GetClient().Update(ctx, &store)
	})
}

// deregisterAuthorizationModelFromStore removes the finalizer added by
// registerAuthorizationModelWithStore. A NotFound Store is treated as
// already deregistered.
func deregisterAuthorizationModelFromStore(ctx context.Context, mgr mcmanager.Manager, storeCluster multicluster.ClusterName, storeName, modelCluster, modelName string) error {
	cl, err := mgr.GetCluster(ctx, storeCluster)
	if err != nil {
		return fmt.Errorf("getting store cluster: %w", err)
	}
	finalizer := authorizationModelFinalizer(modelCluster, modelName)

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var store pmcorev1alpha1.Store
		err := cl.GetClient().Get(ctx, types.NamespacedName{Name: storeName}, &store)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !controllerutil.ContainsFinalizer(&store, finalizer) {
			return nil
		}
		controllerutil.RemoveFinalizer(&store, finalizer)
		return cl.GetClient().Update(ctx, &store)
	})
}
