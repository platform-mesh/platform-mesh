package subroutine

import (
	"context"
	"slices"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	lifecycleruntimeobject "github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/platform-mesh/security-operator/api/v1alpha1"
)

type storeSubroutine struct {
	fga          openfgav1.OpenFGAServiceClient
	k8s          client.Client
	lcClientFunc NewLogicalClusterClientFunc
}

func NewStoreSubroutine(fga openfgav1.OpenFGAServiceClient, k8s client.Client, lcClientFunc NewLogicalClusterClientFunc) *storeSubroutine {
	return &storeSubroutine{
		fga:          fga,
		k8s:          k8s,
		lcClientFunc: lcClientFunc,
	}
}

var _ lifecyclesubroutine.Subroutine = &storeSubroutine{}

func (s *storeSubroutine) GetName() string { return "Store" }

func (s *storeSubroutine) Finalizers() []string { return []string{"core.platform-mesh.io/fga-store"} }

func (s *storeSubroutine) Finalize(ctx context.Context, instance lifecycleruntimeobject.RuntimeObject) (reconcile.Result, errors.OperatorError) {
	log := logger.LoadLoggerFromContext(ctx)
	store := instance.(*v1alpha1.Store)

	if store.Status.StoreID == "" {
		return ctrl.Result{}, nil
	}

	authorizationModels, err := getRelatedAuthorizationModels(ctx, s.k8s, store, s.lcClientFunc)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}
	if len(authorizationModels.Items) != 0 {
		return ctrl.Result{}, errors.NewOperatorError(errors.New("found non-zero count of depending authorization models"), false, false)
	}

	_, err = s.fga.DeleteStore(ctx, &openfgav1.DeleteStoreRequest{StoreId: store.Status.StoreID})
	if status, ok := status.FromError(err); ok && status.Code() == codes.Code(openfgav1.NotFoundErrorCode_store_id_not_found) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		log.Error().Err(err).Msg("unable to delete store")
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	return ctrl.Result{}, nil
}

func (s *storeSubroutine) Process(ctx context.Context, instance lifecycleruntimeobject.RuntimeObject) (reconcile.Result, errors.OperatorError) {
	log := logger.LoadLoggerFromContext(ctx)
	store := instance.(*v1alpha1.Store)

	if store.Status.StoreID == "" {
		log.Info().Msg("Store ID not set, trying to find store by name")

		list, err := s.fga.ListStores(ctx, &openfgav1.ListStoresRequest{})
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, false)
		}

		storeIdx := slices.IndexFunc(list.GetStores(), func(i *openfgav1.Store) bool { return i.GetName() == store.Name })
		if storeIdx != -1 {
			log.Info().Msg("Store found, updating store ID")
			store.Status.StoreID = list.GetStores()[storeIdx].GetId()
			return ctrl.Result{}, nil
		}

		log.Info().Msg("Store not found, creating new store")
		res, err := s.fga.CreateStore(ctx, &openfgav1.CreateStoreRequest{
			Name: store.Name,
		})
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, false)
		}

		store.Status.StoreID = res.GetId()
		return ctrl.Result{}, nil
	}

	fgaStore, err := s.fga.GetStore(ctx, &openfgav1.GetStoreRequest{StoreId: store.Status.StoreID})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	if fgaStore.GetName() == store.Name {
		return ctrl.Result{}, nil
	}
	_, err = s.fga.UpdateStore(ctx, &openfgav1.UpdateStoreRequest{
		StoreId: store.Status.StoreID,
		Name:    store.Name,
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	return ctrl.Result{}, nil
}
