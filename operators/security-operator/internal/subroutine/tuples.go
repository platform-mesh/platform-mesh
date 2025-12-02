package subroutine

import (
	"context"
	"fmt"
	"slices"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/fga/helpers"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/platform-mesh/security-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

type tupleSubroutine struct {
	fga openfgav1.OpenFGAServiceClient
	mgr mcmanager.Manager
}

// Finalize implements lifecycle.Subroutine.
func (t *tupleSubroutine) Finalize(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	log := logger.LoadLoggerFromContext(ctx)

	var storeID string
	var authorizationModelID string
	var managedTuples []v1alpha1.Tuple

	switch obj := instance.(type) {
	case *v1alpha1.Store:
		storeID = obj.Status.StoreID
		authorizationModelID = obj.Status.AuthorizationModelID
		managedTuples = obj.Status.ManagedTuples
	case *v1alpha1.AuthorizationModel:
		cluster, err := t.mgr.ClusterFromContext(ctx)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to get cluster from context: %w", err), true, false)
		}

		managedTuples = obj.Status.ManagedTuples

		var lc kcpcorev1alpha1.LogicalCluster
		err = cluster.GetClient().Get(ctx, client.ObjectKey{Name: "cluster"}, &lc)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		storeCtx := mccontext.WithCluster(ctx, string(logicalcluster.Name(lc.Annotations[logicalcluster.AnnotationKey])))

		var store v1alpha1.Store
		err = cluster.GetClient().Get(storeCtx, types.NamespacedName{
			Name: obj.Spec.StoreRef.Name,
		}, &store)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		storeID = store.Status.StoreID
		authorizationModelID = store.Status.AuthorizationModelID
	}

	for _, tuple := range managedTuples {
		_, err := t.fga.Write(ctx, &openfgav1.WriteRequest{
			StoreId:              storeID,
			AuthorizationModelId: authorizationModelID,
			Deletes: &openfgav1.WriteRequestDeletes{
				TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
					{
						Object:   tuple.Object,
						Relation: tuple.Relation,
						User:     tuple.User,
					},
				},
			},
		})
		if helpers.IsDuplicateWriteError(err) { // coverage-ignore
			log.Info().Stringer("tuple", tuple).Msg("tuple already deleted")
			continue
		}
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, false, true)
		}
	}

	switch obj := instance.(type) {
	case *v1alpha1.Store:
		obj.Status.ManagedTuples = nil
	case *v1alpha1.AuthorizationModel:
		obj.Status.ManagedTuples = nil
	}

	return ctrl.Result{}, nil
}

// Finalizers implements lifecycle.Subroutine.
func (t *tupleSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string {
	return []string{"core.platform-mesh.io/fga-tuples"}
}

// GetName implements lifecycle.Subroutine.
func (t *tupleSubroutine) GetName() string { return "TupleSubroutine" }

// Process implements lifecycle.Subroutine.
func (t *tupleSubroutine) Process(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	log := logger.LoadLoggerFromContext(ctx)

	var storeID string
	var authorizationModelID string
	var specTuples []v1alpha1.Tuple
	var managedTuples []v1alpha1.Tuple

	switch obj := instance.(type) {
	case *v1alpha1.Store:
		storeID = obj.Status.StoreID
		authorizationModelID = obj.Status.AuthorizationModelID

		specTuples = obj.Spec.Tuples
		managedTuples = obj.Status.ManagedTuples
	case *v1alpha1.AuthorizationModel:
		cluster, err := t.mgr.ClusterFromContext(ctx)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to get cluster from context: %w", err), true, false)
		}

		specTuples = obj.Spec.Tuples
		managedTuples = obj.Status.ManagedTuples

		var lc kcpcorev1alpha1.LogicalCluster
		err = cluster.GetClient().Get(ctx, client.ObjectKey{Name: "cluster"}, &lc)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		storeCtx := mccontext.WithCluster(ctx, string(logicalcluster.Name(lc.Annotations[logicalcluster.AnnotationKey])))

		storeCluster, err := t.mgr.GetCluster(ctx, obj.Spec.StoreRef.Path)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("unable to get store cluster: %w", err), true, false)
		}

		var store v1alpha1.Store
		err = storeCluster.GetClient().Get(storeCtx, types.NamespacedName{
			Name: obj.Spec.StoreRef.Name,
		}, &store)
		if err != nil { // coverage-ignore
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		storeID = store.Status.StoreID
		authorizationModelID = store.Status.AuthorizationModelID
	}

	for _, tuple := range specTuples {
		_, err := t.fga.Write(ctx, &openfgav1.WriteRequest{
			StoreId:              storeID,
			AuthorizationModelId: authorizationModelID,
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys: []*openfgav1.TupleKey{
					{
						Object:   tuple.Object,
						Relation: tuple.Relation,
						User:     tuple.User,
					},
				},
			},
		})
		if helpers.IsDuplicateWriteError(err) { // coverage-ignore
			log.Info().Stringer("tuple", tuple).Msg("tuple already exists")
			continue
		}
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, false, true)
		}
	}

	for _, tuple := range managedTuples {
		if idx := slices.IndexFunc(specTuples, func(t v1alpha1.Tuple) bool {
			return t.Object == tuple.Object && t.Relation == tuple.Relation && t.User == tuple.User
		}); idx != -1 {
			continue
		}

		_, err := t.fga.Write(ctx, &openfgav1.WriteRequest{
			StoreId:              storeID,
			AuthorizationModelId: authorizationModelID,
			Deletes: &openfgav1.WriteRequestDeletes{
				TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
					{
						Object:   tuple.Object,
						Relation: tuple.Relation,
						User:     tuple.User,
					},
				},
			},
		})
		if helpers.IsDuplicateWriteError(err) { // coverage-ignore
			log.Info().Stringer("tuple", tuple).Msg("tuple already deleted")
			continue
		}
		if err != nil { // coverage-ignore
			return ctrl.Result{}, errors.NewOperatorError(err, false, true)
		}

	}

	switch obj := instance.(type) {
	case *v1alpha1.Store:
		obj.Status.ManagedTuples = specTuples
	case *v1alpha1.AuthorizationModel:
		obj.Status.ManagedTuples = specTuples
	}

	return ctrl.Result{}, nil
}

func NewTupleSubroutine(fga openfgav1.OpenFGAServiceClient, mgr mcmanager.Manager) *tupleSubroutine {
	return &tupleSubroutine{
		fga: fga,
		mgr: mgr,
	}
}

var _ lifecyclesubroutine.Subroutine = &tupleSubroutine{}
