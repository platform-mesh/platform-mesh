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
	"fmt"
	"slices"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/fga"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/logicalcluster/v3"
)

type tupleSubroutine struct {
	fga openfgav1.OpenFGAServiceClient
	mgr mcmanager.Manager
}

// Finalize implements subroutines.Finalizer.
func (t *tupleSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)

	var storeID string
	var authorizationModelID string
	var managedTuples []pmcorev1alpha1.Tuple

	switch o := obj.(type) {
	case *pmcorev1alpha1.Store:
		storeID = o.Status.StoreID
		authorizationModelID = o.Status.AuthorizationModelID
		managedTuples = o.Status.ManagedTuples
	case *pmcorev1alpha1.AuthorizationModel:
		managedTuples = o.Status.ManagedTuples

		storeCluster, err := t.mgr.GetCluster(ctx, config.StoreClusterName(o.Spec.StoreRef.Cluster))
		if err != nil {
			return subroutines.OK(), fmt.Errorf("unable to get store cluster: %w", err)
		}

		var store pmcorev1alpha1.Store
		err = storeCluster.GetClient().Get(ctx, types.NamespacedName{
			Name: o.Spec.StoreRef.Name,
		}, &store)
		if apierrors.IsNotFound(err) {
			// The Store this AuthorizationModel depended on is already gone, so its
			// tuples and the FGA store itself are already gone too — nothing left to
			// clean up.
			o.Status.ManagedTuples = nil
			return subroutines.OK(), nil
		}
		if err != nil {
			return subroutines.OK(), err
		}

		storeID = store.Status.StoreID
		authorizationModelID = store.Status.AuthorizationModelID
	}

	tm := fga.NewTupleManager(t.fga, storeID, authorizationModelID, log)
	if err := tm.Delete(ctx, managedTuples); err != nil {
		return subroutines.OK(), err
	}

	if o, ok := obj.(*pmcorev1alpha1.AuthorizationModel); ok {
		// Release the Store reservation before this object's own finalizer clears.
		if err := deregisterAuthorizationModelFromStore(ctx, t.mgr,
			config.StoreClusterName(o.Spec.StoreRef.Cluster), o.Spec.StoreRef.Name,
			logicalcluster.From(o).String(), o.GetName(),
		); err != nil {
			return subroutines.OK(), fmt.Errorf("deregistering authorization model from store: %w", err)
		}
	}

	switch o := obj.(type) {
	case *pmcorev1alpha1.Store:
		o.Status.ManagedTuples = nil
	case *pmcorev1alpha1.AuthorizationModel:
		o.Status.ManagedTuples = nil
	}

	return subroutines.OK(), nil
}

// Finalizers implements subroutines.Finalizer.
func (t *tupleSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{"core.platform-mesh.io/fga-tuples"}
}

// GetName implements subroutines.Subroutine.
func (t *tupleSubroutine) GetName() string { return "TupleSubroutine" }

// Process implements subroutines.Processor.
func (t *tupleSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)

	var storeID string
	var authorizationModelID string
	var specTuples []pmcorev1alpha1.Tuple
	var managedTuples []pmcorev1alpha1.Tuple

	switch o := obj.(type) {
	case *pmcorev1alpha1.Store:
		storeID = o.Status.StoreID
		authorizationModelID = o.Status.AuthorizationModelID

		specTuples = o.Spec.Tuples
		managedTuples = o.Status.ManagedTuples
	case *pmcorev1alpha1.AuthorizationModel:
		specTuples = o.Spec.Tuples
		managedTuples = o.Status.ManagedTuples

		storeCluster, err := t.mgr.GetCluster(ctx, config.StoreClusterName(o.Spec.StoreRef.Cluster))
		if err != nil {
			return subroutines.OK(), fmt.Errorf("unable to get store cluster: %w", err)
		}

		var store pmcorev1alpha1.Store
		err = storeCluster.GetClient().Get(ctx, types.NamespacedName{
			Name: o.Spec.StoreRef.Name,
		}, &store)
		if err != nil {
			return subroutines.OK(), err
		}

		storeID = store.Status.StoreID
		authorizationModelID = store.Status.AuthorizationModelID
	}

	tm := fga.NewTupleManager(t.fga, storeID, authorizationModelID, log)
	if err := tm.Apply(ctx, specTuples); err != nil {
		return subroutines.OK(), err
	}

	var tuplesToDelete []pmcorev1alpha1.Tuple
	for _, tuple := range managedTuples {
		if slices.IndexFunc(specTuples, func(s pmcorev1alpha1.Tuple) bool {
			return s.Object == tuple.Object && s.Relation == tuple.Relation && s.User == tuple.User
		}) == -1 {
			tuplesToDelete = append(tuplesToDelete, tuple)
		}
	}
	if err := tm.Delete(ctx, tuplesToDelete); err != nil {
		return subroutines.OK(), err
	}

	switch o := obj.(type) {
	case *pmcorev1alpha1.Store:
		o.Status.ManagedTuples = specTuples
	case *pmcorev1alpha1.AuthorizationModel:
		o.Status.ManagedTuples = specTuples
	}

	return subroutines.OK(), nil
}

func NewTupleSubroutine(fga openfgav1.OpenFGAServiceClient, mgr mcmanager.Manager) *tupleSubroutine {
	return &tupleSubroutine{
		fga: fga,
		mgr: mgr,
	}
}

var _ subroutines.Subroutine = &tupleSubroutine{}
