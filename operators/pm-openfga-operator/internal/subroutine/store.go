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
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/pm-openfga-operator/internal/client"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/logicalcluster/v3"
)

type storeSubroutine struct {
	fga       openfgav1.OpenFGAServiceClient
	mgr       mcmanager.Manager
	kcpHelper iclient.Lister
}

func NewStoreSubroutine(fga openfgav1.OpenFGAServiceClient, mgr mcmanager.Manager, kcpHelper iclient.Lister) *storeSubroutine {
	return &storeSubroutine{
		fga:       fga,
		mgr:       mgr,
		kcpHelper: kcpHelper,
	}
}

var _ subroutines.Subroutine = &storeSubroutine{}

func (s *storeSubroutine) GetName() string { return "Store" }

func (s *storeSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{"core.platform-mesh.io/fga-store"}
}

func (s *storeSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)
	store := obj.(*pmcorev1alpha1.Store)

	if store.Status.StoreID == "" {
		return subroutines.OK(), nil
	}

	// Block deletion while any AuthorizationModel still depends on this
	// Store, tracked via finalizers instead of a list to avoid stale reads.
	for _, f := range store.GetFinalizers() {
		if strings.HasPrefix(f, authorizationModelFinalizerPrefix) {
			// The models go with the Store: nothing else removes them when an
			// organization is deleted. An APIBinding of a workspace that is
			// being deleted cannot be finalized, so the generator does not
			// get to do it. Each model releases its finalizer here once its
			// tuples are gone.
			if err := s.deleteAuthorizationModels(ctx, store); err != nil {
				return subroutines.OK(), fmt.Errorf("deleting dependent authorization models: %w", err)
			}
			return subroutines.OK(), fmt.Errorf("waiting for dependent authorization model to be removed: %s", f)
		}
	}

	_, err := s.fga.DeleteStore(ctx, &openfgav1.DeleteStoreRequest{StoreId: store.Status.StoreID})
	if status, ok := status.FromError(err); ok && status.Code() == codes.Code(openfgav1.NotFoundErrorCode_store_id_not_found) {
		return subroutines.OK(), nil
	}
	if err != nil {
		log.Error().Err(err).Msg("unable to delete store")
		return subroutines.OK(), err
	}

	return subroutines.OK(), nil
}

// deleteAuthorizationModels deletes the AuthorizationModels that extend the
// Store, in the provider workspaces they live in.
func (s *storeSubroutine) deleteAuthorizationModels(ctx context.Context, store *pmcorev1alpha1.Store) error {
	models, err := getRelatedAuthorizationModels(ctx, s.kcpHelper, store)
	if err != nil {
		return fmt.Errorf("listing authorization models: %w", err)
	}

	for i := range models.Items {
		model := &models.Items[i]
		if model.GetDeletionTimestamp() != nil {
			continue
		}

		modelCluster := logicalcluster.From(model).String()
		cluster, err := s.mgr.GetCluster(ctx, config.MultiProviderName(config.ProviderFgaProviderName, modelCluster))
		if err != nil {
			return fmt.Errorf("getting cluster %q of authorization model %s: %w", modelCluster, model.Name, err)
		}
		if err := cluster.GetClient().Delete(ctx, model); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting authorization model %s in %q: %w", model.Name, modelCluster, err)
		}
	}

	return nil
}

func (s *storeSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)
	store := obj.(*pmcorev1alpha1.Store)

	if store.Status.StoreID == "" {
		log.Info().Msg("Store ID not set, trying to find store by name")

		list, err := s.fga.ListStores(ctx, &openfgav1.ListStoresRequest{})
		if err != nil {
			return subroutines.OK(), err
		}

		storeIdx := slices.IndexFunc(list.GetStores(), func(i *openfgav1.Store) bool { return i.GetName() == store.Name })
		if storeIdx != -1 {
			log.Info().Msg("Store found, updating store ID")
			store.Status.StoreID = list.GetStores()[storeIdx].GetId()
			return subroutines.OK(), nil
		}

		log.Info().Msg("Store not found, creating new store")
		res, err := s.fga.CreateStore(ctx, &openfgav1.CreateStoreRequest{
			Name: store.Name,
		})
		if err != nil {
			return subroutines.OK(), err
		}

		store.Status.StoreID = res.GetId()
		return subroutines.OK(), nil
	}

	fgaStore, err := s.fga.GetStore(ctx, &openfgav1.GetStoreRequest{StoreId: store.Status.StoreID})
	if status.Code(err) == codes.Code(openfgav1.NotFoundErrorCode_store_id_not_found) {
		log.Info().Msg("Store ID not found, recreating store")
		res, err := s.fga.CreateStore(ctx, &openfgav1.CreateStoreRequest{Name: store.Name})
		if err != nil {
			return subroutines.OK(), err
		}
		store.Status.StoreID = res.GetId()
		store.Status.AuthorizationModelID = ""
		return subroutines.OK(), nil
	}
	if err != nil {
		return subroutines.OK(), err
	}

	if fgaStore.GetName() == store.Name {
		return subroutines.OK(), nil
	}
	_, err = s.fga.UpdateStore(ctx, &openfgav1.UpdateStoreRequest{
		StoreId: store.Status.StoreID,
		Name:    store.Name,
	})
	if err != nil {
		return subroutines.OK(), err
	}

	return subroutines.OK(), nil
}
