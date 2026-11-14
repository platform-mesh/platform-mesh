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
	"errors"
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmopenfgav1alpha1 "go.platform-mesh.io/apis/openfga/v1alpha1"
	"go.platform-mesh.io/pm-openfga-operator/internal/legacy"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// storeRef is a Store found in kcp: the object as kcp holds it
// (openfga.platform-mesh.io or the deprecated core kind) and its current view.
type storeRef struct {
	client ctrlruntimeclient.Client
	obj    ctrlruntimeclient.Object
	store  *pmopenfgav1alpha1.Store
}

// findStore looks a Store up by its logical cluster and name, first as
// openfga.platform-mesh.io through the openfga provider, then as the
// deprecated core kind through the fga provider.
// Returns a NotFound error when neither exists.
func findStore(ctx context.Context, mgr mcmanager.Manager, logicalCluster, name string) (*storeRef, error) {
	current, err := clusterClient(ctx, mgr, config.StoreClusterName(logicalCluster))
	if err != nil && !errors.Is(err, multicluster.ErrClusterNotFound) {
		return nil, err
	}
	if err == nil {
		var store pmopenfgav1alpha1.Store
		err = current.Get(ctx, types.NamespacedName{Name: name}, &store)
		if err == nil {
			return &storeRef{client: current, obj: &store, store: &store}, nil
		}
		if !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
			return nil, fmt.Errorf("getting Store %s: %w", name, err)
		}
	}

	legacyClient, err := clusterClient(ctx, mgr, config.MultiProviderName(config.FgaProviderName, logicalCluster))
	if errors.Is(err, multicluster.ErrClusterNotFound) {
		// No legacy provider engaged (LegacyAPIs off or cluster not bound): only the current kind counts.
		return nil, apierrors.NewNotFound(pmopenfgav1alpha1.SchemeGroupVersion.WithResource("stores").GroupResource(), name)
	}
	if err != nil {
		return nil, err
	}
	//nolint:staticcheck // legacy kind served on purpose
	var legacyStore pmcorev1alpha1.Store
	if err := legacyClient.Get(ctx, types.NamespacedName{Name: name}, &legacyStore); err != nil {
		if meta.IsNoMatchError(err) {
			return nil, apierrors.NewNotFound(pmopenfgav1alpha1.SchemeGroupVersion.WithResource("stores").GroupResource(), name)
		}
		return nil, fmt.Errorf("getting legacy Store %s: %w", name, err)
	}
	return &storeRef{client: legacyClient, obj: &legacyStore, store: legacy.StoreToCurrent(&legacyStore)}, nil
}

func clusterClient(ctx context.Context, mgr mcmanager.Manager, name multicluster.ClusterName) (ctrlruntimeclient.Client, error) {
	cl, err := mgr.GetCluster(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("getting cluster %s: %w", name, err)
	}
	return cl.GetClient(), nil
}

// modelTarget writes generated AuthorizationModels into a provider workspace
// in the API group it bound: provider-openfga.platform-mesh.io, or the
// deprecated provider-fga.platform-mesh.io when only that one is bound.
type modelTarget struct {
	client ctrlruntimeclient.Client
	// legacyClient reaches the deprecated kind; nil when the cluster
	// is not engaged through the provider-fga provider.
	legacyClient ctrlruntimeclient.Client
}

// findModelTarget resolves the provider workspace logicalCluster.
func findModelTarget(ctx context.Context, mgr mcmanager.Manager, logicalCluster string) (*modelTarget, error) {
	current, err := clusterClient(ctx, mgr, config.MultiProviderName(config.ProviderOpenfgaProviderName, logicalCluster))
	if err != nil && !errors.Is(err, multicluster.ErrClusterNotFound) {
		return nil, err
	}
	legacyClient, legacyErr := clusterClient(ctx, mgr, config.MultiProviderName(config.ProviderFgaProviderName, logicalCluster))
	if legacyErr != nil && !errors.Is(legacyErr, multicluster.ErrClusterNotFound) {
		return nil, legacyErr
	}
	if current == nil && legacyClient == nil {
		return nil, fmt.Errorf("getting cluster %s: %w", logicalCluster, multicluster.ErrClusterNotFound)
	}
	return &modelTarget{client: current, legacyClient: legacyClient}, nil
}

// createOrUpdate writes the model named name with spec. A deprecated model of
// the same name that the current one already adopted is removed.
func (t *modelTarget) createOrUpdate(ctx context.Context, name string, spec pmopenfgav1alpha1.AuthorizationModelSpec) error {
	if t.client == nil {
		//nolint:staticcheck // legacy kind served on purpose
		model := pmcorev1alpha1.AuthorizationModel{ObjectMeta: metav1.ObjectMeta{Name: name}}
		_, err := controllerutil.CreateOrUpdate(ctx, t.legacyClient, &model, func() error {
			model.Spec = legacy.AuthorizationModelSpecFromCurrent(spec)
			return nil
		})
		return err
	}
	model := pmopenfgav1alpha1.AuthorizationModel{ObjectMeta: metav1.ObjectMeta{Name: name}}
	_, err := controllerutil.CreateOrUpdate(ctx, t.client, &model, func() error {
		model.Spec = spec
		return nil
	})
	if err != nil || t.legacyClient == nil {
		return err
	}
	//nolint:staticcheck // legacy kind served on purpose
	var legacyModel pmcorev1alpha1.AuthorizationModel
	err = t.legacyClient.Get(ctx, types.NamespacedName{Name: name}, &legacyModel)
	if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting legacy AuthorizationModel %s: %w", name, err)
	}
	if !legacy.Migrated(&legacyModel) {
		return nil
	}
	if err := t.legacyClient.Delete(ctx, &legacyModel); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting legacy AuthorizationModel %s: %w", name, err)
	}
	return nil
}

// delete removes the model named name from both groups.
// Returns NotFound when neither held it.
func (t *modelTarget) delete(ctx context.Context, name string) error {
	found := false
	if t.client != nil {
		err := t.client.Delete(ctx, &pmopenfgav1alpha1.AuthorizationModel{ObjectMeta: metav1.ObjectMeta{Name: name}})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		found = err == nil
	}
	if t.legacyClient != nil {
		//nolint:staticcheck // legacy kind served on purpose
		err := t.legacyClient.Delete(ctx, &pmcorev1alpha1.AuthorizationModel{ObjectMeta: metav1.ObjectMeta{Name: name}})
		if err != nil && !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
			return err
		}
		found = found || err == nil
	}
	if !found {
		return apierrors.NewNotFound(pmopenfgav1alpha1.SchemeGroupVersion.WithResource("authorizationmodels").GroupResource(), name)
	}
	return nil
}
