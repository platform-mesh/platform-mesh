package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pmmarketplacev1alpha1 "go.platform-mesh.io/apis/marketplace/v1alpha1"

	"k8s.io/apimachinery/pkg/util/sets"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/kcp-dev/logicalcluster/v3"
	mcpcache "github.com/kcp-dev/multicluster-provider/pkg/cache"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

// DenyAllExports is a placeholder for the VisibleExportsFunc.
func DenyAllExports(context.Context, string) (map[string]sets.Set[string], error) {
	return nil, nil
}

func CanonicalHome(
	clusterByPath func(ctx context.Context, path logicalcluster.Path) (ctrlruntimeclient.Client, error),
	homePattern string,
) VisibleExportsFunc {
	return func(ctx context.Context, logicalClusterID string) (map[string]sets.Set[string], error) {
		path, got := ClusterPathFrom(ctx)
		if !got {
			return nil, errors.New("get canonical home: no cluster path in context")
		}

		result := make(map[string]sets.Set[string])

		if !strings.HasPrefix(path.String(), homePattern) {
			return result, nil
		}

		tenantPath := strings.TrimPrefix(path.String(), homePattern+":")

		pathSegments := strings.Split(tenantPath, ":")
		if len(pathSegments) <= 1 {
			return result, nil
		}

		grantsHomeSegment := pathSegments[1]
		home := fmt.Sprintf("%s:%s", homePattern, grantsHomeSegment)

		homePath := logicalcluster.NewPath(home)
		homeClient, err := clusterByPath(ctx, homePath)
		if err != nil {
			if errors.Is(err, multicluster.ErrClusterNotFound) {
				return result, nil
			}
			return nil, err
		}

		var grants pmmarketplacev1alpha1.VisibilityGrantList
		err = homeClient.List(ctx, &grants)
		if err != nil {
			return nil, err
		}

		for _, grantObj := range grants.Items {
			for _, provider := range grantObj.Spec.Providers {
				entries, got := result[provider.ProviderClusterID]
				if !got {
					entries = make(sets.Set[string])
				}
				entries.Insert(provider.APIExports...)
				result[provider.ProviderClusterID] = entries
			}
		}

		return result, nil
	}
}

func VisibleExportsFromGrants(
	client mcpcache.Lister,
	getLogicalCluster func(ctx context.Context, clusterID string) (*kcpcorev1alpha1.LogicalCluster, error),
) VisibleExportsFunc {
	return func(ctx context.Context, logicalClusterID string) (map[string]sets.Set[string], error) {
		// ancestors:
		clusterIDs := make(sets.Set[string])
		clusterID := logicalClusterID
		for !clusterIDs.Has(clusterID) {
			if clusterID == core.RootCluster.String() {
				clusterIDs.Insert(clusterID)
				break
			}
			cl, err := getLogicalCluster(ctx, clusterID)
			if err != nil {
				return nil, fmt.Errorf("retrieving logical cluster by ID %q: %w", clusterID, err)
			}
			if cl.Spec.Owner == nil {
				// TBD: fails the whole request, maybe no error here is better?
				return nil, fmt.Errorf("non-root workspace %q has no owner", clusterID)
			}

			clusterIDs.Insert(clusterID)
			clusterID = cl.Spec.Owner.Cluster
		}

		var allGrants pmmarketplacev1alpha1.VisibilityGrantList

		err := client.List(ctx, &allGrants)
		if err != nil {
			return nil, fmt.Errorf("listing all VisibilityGrants: %w", err)
		}

		result := make(map[string]sets.Set[string])

		for _, grantObj := range allGrants.Items {
			grantCluster := logicalcluster.From(&grantObj)

			if clusterIDs.Has(grantCluster.String()) {
				for _, provider := range grantObj.Spec.Providers {
					entries, got := result[provider.ProviderClusterID]
					if !got {
						entries = make(sets.Set[string])
					}
					entries.Insert(provider.APIExports...)
					result[provider.ProviderClusterID] = entries
				}
			}
		}

		return result, nil
	}
}
