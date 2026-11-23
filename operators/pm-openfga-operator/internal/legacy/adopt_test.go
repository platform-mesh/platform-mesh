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

package legacy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmopenfgav1alpha1 "go.platform-mesh.io/apis/openfga/v1alpha1"
	"go.platform-mesh.io/pm-openfga-operator/internal/legacy"
	"go.platform-mesh.io/pm-openfga-operator/internal/subroutine/mocks"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

const (
	storeName         = "acme"
	storeFinalizer    = "core.platform-mesh.io/fga-store"
	dependencyOfModel = legacy.DependencyFinalizerPrefix + "0123"
)

//nolint:staticcheck // legacy kind served on purpose
func legacyStore(migrated bool, storeID string, finalizers ...string) *pmcorev1alpha1.Store {
	store := &pmcorev1alpha1.Store{
		ObjectMeta: metav1.ObjectMeta{Name: storeName, Finalizers: finalizers},
		Status:     pmcorev1alpha1.StoreStatus{StoreID: storeID},
	}
	if migrated {
		store.Annotations = map[string]string{legacy.MigratedAnnotation: "true"}
	}
	return store
}

func currentStore(storeID string) *pmopenfgav1alpha1.Store {
	return &pmopenfgav1alpha1.Store{
		ObjectMeta: metav1.ObjectMeta{Name: storeName},
		Status:     pmopenfgav1alpha1.StoreStatus{StoreID: storeID},
	}
}

// adopt runs the StoreAdopter for current against a logical cluster that
// holds the deprecated Store, and returns the client of that cluster.
//
//nolint:staticcheck // legacy kind served on purpose
func adopt(t *testing.T, deprecated *pmcorev1alpha1.Store, current *pmopenfgav1alpha1.Store) ctrlruntimeclient.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, pmcorev1alpha1.AddToScheme(scheme))
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deprecated).Build()

	cluster := mocks.NewMockCluster(t)
	cluster.EXPECT().GetClient().Return(cl)
	mgr := mocks.NewMockManager(t)
	mgr.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.FgaProviderName, "orgs")).Return(cluster, nil)

	ctx := mccontext.WithCluster(context.Background(), config.StoreClusterName("orgs"))
	_, err := legacy.StoreAdopter(mgr).(subroutines.Processor).Process(ctx, current)
	require.NoError(t, err)
	return cl
}

//nolint:staticcheck // legacy kind served on purpose
func TestStoreAdopter(t *testing.T) {
	key := ctrlruntimeclient.ObjectKey{Name: storeName}

	t.Run("hands over and keeps the deprecated Store until the hand-over is stored", func(t *testing.T) {
		current := currentStore("")
		cl := adopt(t, legacyStore(false, "store-1", storeFinalizer, dependencyOfModel), current)

		assert.Equal(t, "store-1", current.Status.StoreID)
		assert.Contains(t, current.Finalizers, dependencyOfModel)

		got := &pmcorev1alpha1.Store{}
		require.NoError(t, cl.Get(context.Background(), key, got))
		assert.True(t, legacy.Migrated(got))
		assert.Equal(t, []string{storeFinalizer}, got.Finalizers)
		assert.Nil(t, got.DeletionTimestamp)
	})

	t.Run("deletes the migrated Store once the successor holds its store", func(t *testing.T) {
		cl := adopt(t, legacyStore(true, "store-1", storeFinalizer), currentStore("store-1"))

		// The finalizer keeps it until the legacy reconciler releases it.
		got := &pmcorev1alpha1.Store{}
		require.NoError(t, cl.Get(context.Background(), key, got))
		assert.NotNil(t, got.DeletionTimestamp)
	})

	t.Run("deletes a migrated Store without finalizers", func(t *testing.T) {
		cl := adopt(t, legacyStore(true, "store-1"), currentStore("store-1"))

		err := cl.Get(context.Background(), key, &pmcorev1alpha1.Store{})
		assert.True(t, apierrors.IsNotFound(err), "got %v", err)
	})

	for name, tc := range map[string]struct {
		deprecated *pmcorev1alpha1.Store
		current    *pmopenfgav1alpha1.Store
	}{
		"the successor has no store yet":          {legacyStore(true, "store-1", storeFinalizer), currentStore("")},
		"the successor is on another store":       {legacyStore(true, "store-1", storeFinalizer), currentStore("store-2")},
		"a model reservation was not handed over": {legacyStore(true, "store-1", storeFinalizer, dependencyOfModel), currentStore("store-1")},
	} {
		t.Run("keeps the migrated Store while "+name, func(t *testing.T) {
			cl := adopt(t, tc.deprecated, tc.current)

			got := &pmcorev1alpha1.Store{}
			require.NoError(t, cl.Get(context.Background(), key, got))
			assert.Nil(t, got.DeletionTimestamp)
		})
	}
}
