package storage

import (
	"context"
	"fmt"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmmarketplacev1alpha1 "go.platform-mesh.io/apis/marketplace/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

// makeHierarchy creates a tree of logical clusters where each entry is a child of the previous one.
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

func TestCanonicalHome_FooDbg(t *testing.T) {
	scheme := marketplaceTestScheme(t)

	homeID := "org-foo-id"

	grant := makeGrant(t, homeID, pmmarketplacev1alpha1.ProviderExports{
		ProviderClusterID: "baz-provider-id",
		APIExports:        []string{"buckets.baz"},
	})
	orgClusterClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(&grant).Build()

	// todo: clean up: not unnecessary, just for showcase purposes:
	targetClusterClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects().Build()
	seed := map[string]ctrlruntimeclient.Client{
		"root:orgs:foo-corp":                   orgClusterClient,
		"root:orgs:foo-corp:engineering:team1": targetClusterClient,
	}

	ctx := WithClusterPath(t.Context(), logicalcluster.NewPath("root:orgs:foo-corp:engineering:team1"))
	sut := CanonicalHome(clustersByPath(seed), "root:orgs")

	result, err := sut(ctx, "team-cluster-id")
	require.NoError(t, err)
	require.Len(t, result, 1)

	assert.True(t, result["baz-provider-id"].Has("buckets.baz"))

}

func clustersByPath(
	clusterClients map[string]ctrlruntimeclient.Client,
) func(ctx context.Context, path logicalcluster.Path) (ctrlruntimeclient.Client, error) {

	return func(ctx context.Context, path logicalcluster.Path) (ctrlruntimeclient.Client, error) {
		cc, got := clusterClients[path.String()]
		if !got {
			// make sure the sentinel error is as expected:
			return nil, fmt.Errorf("cluster by path %q not found: %w",
				path.String(), multicluster.ErrClusterNotFound)
		}
		return cc, nil
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

func TestVisibleExportsFromGrants_AggregatingPRoviderGrants(t *testing.T) {
	scheme := marketplaceTestScheme(t)

	t.Run("single provider with export grants across two workspaces", func(t *testing.T) {
		parent := "foo-id"
		targetws := "bar-id"
		workspaces := makeHierarchy(parent, targetws)

		provider := "foo-storage"
		firstExport := "disks.foo"
		first := pmmarketplacev1alpha1.ProviderExports{
			ProviderClusterID: provider,
			APIExports:        []string{firstExport},
		}
		firstGrant := makeGrant(t, parent, first)

		secondExport := "snapshots.foo"
		second := pmmarketplacev1alpha1.ProviderExports{
			ProviderClusterID: provider,
			APIExports:        []string{secondExport},
		}
		secondGrant := makeGrant(t, targetws, second)

		lister := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(&firstGrant, &secondGrant).Build()

		fn := VisibleExportsFromGrants(lister, makeClusterFn(workspaces...))

		result, err := fn(t.Context(), targetws)
		require.NoError(t, err)
		require.Len(t, result, 1, "should be single provider in map")

		exports, got := result[provider]
		require.True(t, got)
		require.Len(t, exports, 2, "an export should not be missing")

		assert.True(t, exports.HasAll(firstExport, secondExport))
	})
	t.Run("two providers across workspaces", func(t *testing.T) {
		parent := "org-foo-id"
		targetWS := "team-engineering-id"
		workspaces := makeHierarchy(parent, targetWS)

		firstProvider := "org-shared-foo"
		firstExport := "buckets.foo"
		firstGrant := makeGrant(t, parent, pmmarketplacev1alpha1.ProviderExports{
			ProviderClusterID: firstProvider,
			APIExports:        []string{firstExport},
		})

		secondProvider := "team-eng-id"
		secondExport := "whiteboards"
		secondGrant := makeGrant(t, targetWS, pmmarketplacev1alpha1.ProviderExports{
			ProviderClusterID: secondProvider,
			APIExports:        []string{secondExport},
		})

		lister := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(&firstGrant, &secondGrant).Build()
		fn := VisibleExportsFromGrants(lister, makeClusterFn(workspaces...))

		result, err := fn(t.Context(), targetWS)
		require.NoError(t, err)
		require.Len(t, result, 2, "a provider is missing")

		parentGranted := result[firstProvider]
		assert.True(t, parentGranted.Has(firstExport))

		targetWSGranted := result[secondProvider]
		assert.True(t, targetWSGranted.Has(secondExport))
	})
	t.Run("multiple grants in a single workspace", func(t *testing.T) {
		targetWS := "baz-id"
		workspaces := makeHierarchy(targetWS)

		provider := "coffee"
		firstExport := "more.coffee"
		secondExport := "less.coffee"

		firstGrant := makeGrant(t, targetWS, pmmarketplacev1alpha1.ProviderExports{
			ProviderClusterID: provider,
			APIExports:        []string{firstExport},
		})
		secondGrant := makeGrant(t, targetWS, pmmarketplacev1alpha1.ProviderExports{
			ProviderClusterID: provider,
			APIExports:        []string{secondExport},
		})

		lister := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(&firstGrant, &secondGrant).Build()
		fn := VisibleExportsFromGrants(lister, makeClusterFn(workspaces...))

		result, err := fn(t.Context(), targetWS)
		require.NoError(t, err)
		require.Len(t, result, 1, "should not result in duplicate provider entries")
		require.True(t, result[provider].HasAll(firstExport, secondExport))
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

func TestCanonicalHome_InvalidLocations(t *testing.T) {
	scheme := marketplaceTestScheme(t)

	grant := makeGrant(t, "org-foo-id", pmmarketplacev1alpha1.ProviderExports{
		ProviderClusterID: "baz-provider-id",
		APIExports:        []string{"buckets.baz"},
	})
	homeClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(&grant).Build()

	sut := CanonicalHome(clustersByPath(map[string]ctrlruntimeclient.Client{
		"root:orgs:foo-corp": homeClient,
	}), "root:orgs")

	tests := []struct {
		name        string
		requestPath string
	}{
		{"request from root", "root"},
		{"request from the home pattern level", "root:orgs"},
		{"unrelated workspace tree", "root:compute:foo-corp"},
		{"pattern partial match", "root:orgs2:foo-corp:team1"},
		{"sibling org without the binding", "root:orgs:unrelated-org:team1"},
		{"empty request path", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithClusterPath(t.Context(), logicalcluster.NewPath(tc.requestPath))

			result, err := sut(ctx, "some-cluster-id")
			require.NoError(t, err, "denied locations should result in empty response and not an err")
			assert.Empty(t, result, "grants of root:orgs:foo-corp must not return for %q", tc.requestPath)
		})
	}
}

func TestCanonicalHome_Errors(t *testing.T) {
	t.Run("missing cluster path in context", func(t *testing.T) {
		sut := CanonicalHome(clustersByPath(nil), "root:orgs")

		result, err := sut(t.Context(), "some-cluster-id")
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("unexpected resolution errors propagate", func(t *testing.T) {
		connErr := syscall.ECONNREFUSED
		sut := CanonicalHome(func(context.Context, logicalcluster.Path) (ctrlruntimeclient.Client, error) {
			return nil, connErr
		}, "root:orgs")

		ctx := WithClusterPath(t.Context(), logicalcluster.NewPath("root:orgs:foo-corp:team1"))

		result, err := sut(ctx, "some-cluster-id")
		require.ErrorIs(t, err, connErr)
		assert.Nil(t, result)
	})
}
