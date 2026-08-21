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

func CanonicalHome(
	clusterByPath func(ctx context.Context, path logicalcluster.Path) (ctrlruntimeclient.Client, error),
	homePattern string,
) VisibleExportsFunc {
	homePattern = strings.Trim(homePattern, ":")

	return func(ctx context.Context, logicalClusterID string) (map[string]sets.Set[string], error) {
		path, got := ClusterPathFrom(ctx)
		if !got {
			return nil, errors.New("get canonical home: no cluster path in context")
		}

		result := make(map[string]sets.Set[string])

		homePath, found := workspaceHome(path, homePattern)
		if !found {
			return result, nil
		}

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

func workspaceHome(path logicalcluster.Path, homePattern string) (logicalcluster.Path, bool) {
	remainder, found := strings.CutPrefix(path.String(), homePattern+":")
	if !found {
		return logicalcluster.Path{}, false
	}

	home, _, _ := strings.Cut(remainder, ":")
	if home == "" {
		return logicalcluster.Path{}, false
	}
	return logicalcluster.NewPath(homePattern + ":" + home), true
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
