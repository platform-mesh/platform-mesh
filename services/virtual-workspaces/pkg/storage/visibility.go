package storage

import (
	"context"
	"fmt"

	pmmarketplacev1alpha1 "go.platform-mesh.io/apis/marketplace/v1alpha1"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kcp-dev/logicalcluster/v3"
	mcpcache "github.com/kcp-dev/multicluster-provider/pkg/cache"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

// DenyAllExports is a placeholder for the VisibleExportsFunc.
func DenyAllExports(context.Context, string) (map[string]sets.Set[string], error) {
	return nil, nil
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
