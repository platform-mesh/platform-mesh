package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmmarketplacev1alpha1 "go.platform-mesh.io/apis/marketplace/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func makeHierarchy(children ...string) []*kcpcorev1alpha1.LogicalCluster {
	makeCluster := func(name string, ownerLC *kcpcorev1alpha1.LogicalCluster) kcpcorev1alpha1.LogicalCluster {
		var owner *kcpcorev1alpha1.LogicalClusterOwner
		if ownerLC != nil {
			owner = &kcpcorev1alpha1.LogicalClusterOwner{
				Name:    kcpcorev1alpha1.LogicalClusterName,
				Cluster: logicalcluster.From(ownerLC).String(),
			}
		}

		lc := kcpcorev1alpha1.LogicalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: kcpcorev1alpha1.LogicalClusterName},
			Spec: kcpcorev1alpha1.LogicalClusterSpec{
				Owner: owner,
			},
		}
		lc.SetAnnotations(map[string]string{logicalcluster.AnnotationKey: name})
		return lc
	}

	result := make([]*kcpcorev1alpha1.LogicalCluster, 0, len(children)+1)

	root := makeCluster(core.RootCluster.String(), nil)
	result = append(result, &root)

	for _, v := range children {
		child := makeCluster(v, result[len(result)-1])
		result = append(result, &child)
	}
	return result
}

// makeClusterFn returns a function for the retrieval of a cluster by its logical cluster ID.
func makeClusterFn(clusters ...*kcpcorev1alpha1.LogicalCluster) func(ctx context.Context, clusterID string) (*kcpcorev1alpha1.LogicalCluster, error) {
	data := makeClusterMap(clusters...)

	return func(ctx context.Context, clusterID string) (*kcpcorev1alpha1.LogicalCluster, error) {
		lc, got := data[clusterID]
		if !got {
			return nil, fmt.Errorf("cluster with ID %q not found", clusterID)
		}

		return lc, nil
	}
}

func TestVisibleExportsFromGrants(t *testing.T) {
	scheme := marketplaceTestScheme(t)

	t.Run("workspace with direct grant", func(t *testing.T) {
		targetWS := "cluster-foo"
		workspaces := makeHierarchy("foo-parent-id", targetWS)

		provider := "f123-bins"
		expectedExport := "blappers.kcp.dev"

		providerExport := pmmarketplacev1alpha1.ProviderExports{
			ProviderClusterID: provider,
			APIExports:        []string{expectedExport},
		}
		grant := makeGrant(t, targetWS, providerExport)

		lister := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(&grant).
			Build()

		fn := VisibleExportsFromGrants(lister, makeClusterFn(workspaces...))

		result, err := fn(t.Context(), targetWS)
		require.NoError(t, err)
		require.Len(t, result, 1)

		resultExports := result[provider]
		require.Len(t, resultExports, 1)
		assert.True(t, resultExports.Has(expectedExport))
	})
	t.Run("mid-level parent has grant", func(t *testing.T) {
		targetWS := "cluster-bar"
		grantedWS := "cluster-eng"
		workspaces := makeHierarchy("subroot", grantedWS, "other", targetWS)

		provider := "storager"
		expectedExport := "buckets.storager.foo"
		providerExport := pmmarketplacev1alpha1.ProviderExports{
			ProviderClusterID: provider,
			APIExports:        []string{expectedExport},
		}
		grant := makeGrant(t, grantedWS, providerExport)

		lister := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(&grant).
			Build()

		fn := VisibleExportsFromGrants(lister, makeClusterFn(workspaces...))

		result, err := fn(t.Context(), targetWS)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.True(t, result[provider].Has(expectedExport))
	})

	t.Run("grant at root cluster", func(t *testing.T) {
		targetWS := "some-baz-id"
		workspaces := makeHierarchy("parent-id", targetWS)

		provider := "foobar-id"
		export := "backups.foo.bar"
		providerExport := pmmarketplacev1alpha1.ProviderExports{
			ProviderClusterID: provider,
			APIExports:        []string{export},
		}
		grant := makeGrant(t, core.RootCluster.String(), providerExport)

		lister := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(&grant).Build()

		fn := VisibleExportsFromGrants(lister, makeClusterFn(workspaces...))

		result, err := fn(t.Context(), targetWS)
		require.NoError(t, err)
		require.Len(t, result, 1, "should include exports in root workspace")
		assert.True(t, result[provider].Has(export))
	})
	t.Run("corrupted cluster graph", func(t *testing.T) {
		// root, a, b, c
		workspaces := makeHierarchy("a", "b", "c")

		// change node b parent from a to c:
		workspaces[2].Spec.Owner.Cluster = "c"

		lister := fake.NewClientBuilder().WithScheme(scheme).Build()

		fn := VisibleExportsFromGrants(lister, makeClusterFn(workspaces...))

		result, err := fn(t.Context(), "c")
		require.NoError(t, err, "should not error on broken cluster graph")
		require.Empty(t, result)
	})
}

func makeGrant(t *testing.T, clusterID string, providers ...pmmarketplacev1alpha1.ProviderExports) pmmarketplacev1alpha1.VisibilityGrant {
	t.Helper()
	grant := pmmarketplacev1alpha1.VisibilityGrant{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("grant-%s-%s", clusterID, rand.String(8))},
		Spec: pmmarketplacev1alpha1.VisibilityGrantSpec{
			Providers: providers,
		},
	}
	grant.SetAnnotations(map[string]string{logicalcluster.AnnotationKey: clusterID})
	return grant
}

func makeClusterMap(clusters ...*kcpcorev1alpha1.LogicalCluster) map[string]*kcpcorev1alpha1.LogicalCluster {
	result := make(map[string]*kcpcorev1alpha1.LogicalCluster)
	for _, v := range clusters {
		result[logicalcluster.From(v).String()] = v
	}
	return result
}
