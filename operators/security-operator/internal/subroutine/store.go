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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
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

	authorizationModels, err := getRelatedAuthorizationModels(ctx, s.kcpHelper, store)
	if err != nil {
		return subroutines.OK(), err
	}
	if len(authorizationModels.Items) != 0 {
		return subroutines.OK(), fmt.Errorf("found non-zero count of depending authorization models")
	}

	_, err = s.fga.DeleteStore(ctx, &openfgav1.DeleteStoreRequest{StoreId: store.Status.StoreID})
	if status, ok := status.FromError(err); ok && status.Code() == codes.Code(openfgav1.NotFoundErrorCode_store_id_not_found) {
		return subroutines.OK(), nil
	}
	if err != nil {
		log.Error().Err(err).Msg("unable to delete store")
		return subroutines.OK(), err
	}

	return subroutines.OK(), nil
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
