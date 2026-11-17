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

package marketplace

import (
	"context"
	"fmt"
	"slices"

	pmmarketplacev1alpha1 "go.platform-mesh.io/apis/marketplace/v1alpha1"
	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/marketplace/pkg/config"

	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
	mcpcache "github.com/kcp-dev/multicluster-provider/pkg/cache"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/forwardingregistry"
)

// Entries replaces the list of the MarketplaceEntry storage: one entry per
// APIExport of every provider (ProviderMetadata), with the name of the
// APIBinding in the requested workspace if the export is bound there.
func Entries(
	lister mcpcache.Lister,
	clusterClient func(context.Context, string) (ctrlruntimeclient.Client, error),
	cfg config.Config,
) forwardingregistry.StorageWrapper {
	return forwardingregistry.StorageWrapperFunc(func(resource schema.GroupResource, storage *forwardingregistry.StoreFuncs) {
		storage.ListerFunc = func(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
			cluster := genericapirequest.ClusterFrom(ctx)

			cl, err := clusterClient(ctx, cluster.Name.String())
			if err != nil {
				return nil, fmt.Errorf("failed to get cluster from provider: %w", err)
			}

			// Get APIBindings for this specific cluster
			installedAPIBindings := &kcpapisv1alpha1.APIBindingList{}
			if err := cl.List(ctx, installedAPIBindings); err != nil {
				return nil, fmt.Errorf("failed to list apibindings: %w", err)
			}

			var providerList pmuiv1alpha1.ProviderMetadataList
			if err := lister.List(ctx, &providerList); err != nil {
				return nil, fmt.Errorf("failed to list providermetadatas: %w", err)
			}

			var results unstructured.UnstructuredList
			results.SetGroupVersionKind(pmmarketplacev1alpha1.SchemeGroupVersion.WithKind("MarketplaceEntryList"))

			// For each provider, find matching APIExports across all shards
			for _, provider := range providerList.Items {
				exportList := &kcpapisv1alpha2.APIExportList{}

				if err := lister.List(ctx, exportList, &ctrlruntimeclient.ListOptions{
					LabelSelector: labels.SelectorFromValidatedSet(map[string]string{
						cfg.ContentForLabel: provider.GetName(),
					}),
				}); err != nil {
					return nil, fmt.Errorf("failed to list apiexports for provider %s: %w", provider.GetName(), err)
				}

				providerClusterID := logicalcluster.From(&provider)
				for _, export := range exportList.Items {
					if logicalcluster.From(&export) != providerClusterID {
						continue
					}
					if export.Status.IdentityHash == "" {
						continue
					}

					idx := slices.IndexFunc(installedAPIBindings.Items, func(item kcpapisv1alpha1.APIBinding) bool {
						return item.Spec.Reference.Export.Name == export.Name &&
							item.Status.APIExportClusterName == logicalcluster.From(&export).String()
					})

					var apiBindingName string
					if idx != -1 {
						apiBindingName = installedAPIBindings.Items[idx].Name
					}

					provider.ManagedFields = nil // clear managed fields to declutter the output
					export.ManagedFields = nil

					unstructuredEntry, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pmmarketplacev1alpha1.MarketplaceEntry{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("%s-%s", export.Name, provider.Name), // TODO: we might need to fix the name length to not exceed the kubernetes limit
						},
						Spec: pmmarketplacev1alpha1.MarketplaceEntrySpec{
							ProviderMetadata: *provider.DeepCopy(),
							APIExport:        *export.DeepCopy(),
							APIBindingName:   apiBindingName,
						},
					})
					if err != nil {
						return nil, fmt.Errorf("failed to convert marketplace entry to unstructured for export %s and provider %s: %w", export.Name, provider.Name, err)
					}

					us := unstructured.Unstructured{Object: unstructuredEntry}
					us.SetGroupVersionKind(pmmarketplacev1alpha1.SchemeGroupVersion.WithKind("MarketplaceEntry"))
					results.Items = append(results.Items, us)
				}
			}
			return &results, nil
		}
	})
}
