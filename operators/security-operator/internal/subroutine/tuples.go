package subroutine

import (
	"context"
	"fmt"
	"slices"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/security-operator/internal/fga"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"k8s.io/apimachinery/pkg/types"
)

type tupleSubroutine struct {
	fga openfgav1.OpenFGAServiceClient
	mgr mcmanager.Manager
}

// Finalize implements subroutines.Finalizer.
func (t *tupleSubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)

	var storeID string
	var authorizationModelID string
	var managedTuples []corev1alpha1.Tuple

	switch o := obj.(type) {
	case *corev1alpha1.Store:
		storeID = o.Status.StoreID
		authorizationModelID = o.Status.AuthorizationModelID
		managedTuples = o.Status.ManagedTuples
	case *corev1alpha1.AuthorizationModel:
		managedTuples = o.Status.ManagedTuples

		storeCluster, err := t.mgr.GetCluster(ctx, multicluster.ClusterName(o.Spec.StoreRef.Cluster))
		if err != nil {
			return subroutines.OK(), fmt.Errorf("unable to get store cluster: %w", err)
		}

		var store corev1alpha1.Store
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
	if err := tm.Delete(ctx, managedTuples); err != nil {
		return subroutines.OK(), err
	}

	switch o := obj.(type) {
	case *corev1alpha1.Store:
		o.Status.ManagedTuples = nil
	case *corev1alpha1.AuthorizationModel:
		o.Status.ManagedTuples = nil
	}

	return subroutines.OK(), nil
}

// Finalizers implements subroutines.Finalizer.
func (t *tupleSubroutine) Finalizers(_ client.Object) []string {
	return []string{"core.platform-mesh.io/fga-tuples"}
}

// GetName implements subroutines.Subroutine.
func (t *tupleSubroutine) GetName() string { return "TupleSubroutine" }

// Process implements subroutines.Processor.
func (t *tupleSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)

	var storeID string
	var authorizationModelID string
	var specTuples []corev1alpha1.Tuple
	var managedTuples []corev1alpha1.Tuple

	switch o := obj.(type) {
	case *corev1alpha1.Store:
		storeID = o.Status.StoreID
		authorizationModelID = o.Status.AuthorizationModelID

		specTuples = o.Spec.Tuples
		managedTuples = o.Status.ManagedTuples
	case *corev1alpha1.AuthorizationModel:
		specTuples = o.Spec.Tuples
		managedTuples = o.Status.ManagedTuples

		storeCluster, err := t.mgr.GetCluster(ctx, multicluster.ClusterName(o.Spec.StoreRef.Cluster))
		if err != nil {
			return subroutines.OK(), fmt.Errorf("unable to get store cluster: %w", err)
		}

		var store corev1alpha1.Store
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

	var tuplesToDelete []corev1alpha1.Tuple
	for _, tuple := range managedTuples {
		if slices.IndexFunc(specTuples, func(s corev1alpha1.Tuple) bool {
			return s.Object == tuple.Object && s.Relation == tuple.Relation && s.User == tuple.User
		}) == -1 {
			tuplesToDelete = append(tuplesToDelete, tuple)
		}
	}
	if err := tm.Delete(ctx, tuplesToDelete); err != nil {
		return subroutines.OK(), err
	}

	switch o := obj.(type) {
	case *corev1alpha1.Store:
		o.Status.ManagedTuples = specTuples
	case *corev1alpha1.AuthorizationModel:
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
