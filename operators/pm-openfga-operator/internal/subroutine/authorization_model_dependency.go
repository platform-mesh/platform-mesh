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

	"go.platform-mesh.io/pm-openfga-operator/internal/legacy"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

// authorizationModelFinalizerPrefix marks the finalizers a Store carries for
// each AuthorizationModel that depends on it.
const authorizationModelFinalizerPrefix = legacy.DependencyFinalizerPrefix

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
// storeCluster is the Store's logical cluster; the Store is looked up as
// openfga.platform-mesh.io first and as the deprecated core kind second.
func registerAuthorizationModelWithStore(ctx context.Context, mgr mcmanager.Manager, storeCluster, storeName, modelCluster, modelName string) error {
	finalizer := authorizationModelFinalizer(modelCluster, modelName)

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ref, err := findStore(ctx, mgr, storeCluster, storeName)
		if err != nil {
			return err
		}
		if controllerutil.ContainsFinalizer(ref.obj, finalizer) {
			return nil
		}
		controllerutil.AddFinalizer(ref.obj, finalizer)
		return ref.client.Update(ctx, ref.obj)
	})
}

// deregisterAuthorizationModelFromStore removes the finalizer added by
// registerAuthorizationModelWithStore. A NotFound Store is treated as
// already deregistered.
func deregisterAuthorizationModelFromStore(ctx context.Context, mgr mcmanager.Manager, storeCluster, storeName, modelCluster, modelName string) error {
	finalizer := authorizationModelFinalizer(modelCluster, modelName)

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ref, err := findStore(ctx, mgr, storeCluster, storeName)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !controllerutil.ContainsFinalizer(ref.obj, finalizer) {
			return nil
		}
		controllerutil.RemoveFinalizer(ref.obj, finalizer)
		return ref.client.Update(ctx, ref.obj)
	})
}
