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

package storage

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kcp-dev/client-go/dynamic"
	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/forwardingregistry"
	"go.platform-mesh.io/apis/marketplace/v1alpha1"
	extensionapiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/virtual-workspaces/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	kerrors "k8s.io/apimachinery/pkg/api/errors"

	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
)

type clusterPathKey struct{}

func WithClusterPath(ctx context.Context, path logicalcluster.Path) context.Context {
	return context.WithValue(ctx, clusterPathKey{}, path)
}

func ClusterPathFrom(ctx context.Context) (logicalcluster.Path, bool) {
	path, ok := ctx.Value(clusterPathKey{}).(logicalcluster.Path)
	if !ok {
		return logicalcluster.Path{}, false
	}
	return path, true
}

func contentConfigurationWithResult(cc *unstructured.UnstructuredList) []unstructured.Unstructured {

	// TODO: this works with unstructed and breaks on api changes, maybe we parse into typed structs instead
	var results []unstructured.Unstructured
	for _, cc := range cc.Items {
		_, hasField, err := unstructured.NestedFieldNoCopy(cc.Object, "status", "configurationResult")
		if err != nil || !hasField {
			klog.V(8).Info(err, "failed to get configurationResult from contentconfiguration", "cc", cc.GetName())
			continue
		}

		results = append(results, cc)
	}

	return results
}

func ContentConfigurationLookup(client dynamic.ClusterInterface, cfg config.ServiceConfig, providerWorkspaceID string) forwardingregistry.StorageWrapper {

	return forwardingregistry.StorageWrapperFunc(func(resource schema.GroupResource, storage *forwardingregistry.StoreFuncs) {
		delegateLister := storage.ListerFunc
		storage.ListerFunc = func(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {

			// Exclude CCs with content-for label from the current workspace.
			// These are provider-published CCs projected via APIBindings and will be
			// fetched from their source export workspaces below with proper filtering.
			localOpts := options.DeepCopy()
			noContentFor, err := labels.Parse("!" + cfg.ContentForLabel)
			if err != nil {
				return nil, err
			}
			if localOpts.LabelSelector != nil {
				reqs, selectable := localOpts.LabelSelector.Requirements()
				if !selectable {
					return &unstructured.UnstructuredList{}, nil
				}
				noContentFor = noContentFor.Add(reqs...)
			}
			localOpts.LabelSelector = noContentFor

			result, err := delegateLister.List(ctx, localOpts)
			if err != nil {
				return nil, err
			}

			ul, _ := result.(*unstructured.UnstructuredList)
			ul.Items = contentConfigurationWithResult(ul)

			path, ok := ClusterPathFrom(ctx)
			if !ok {
				klog.Error("cluster path not found in context")
				return nil, kerrors.NewBadRequest("cluster path not found in context")
			}

			apiBindings, err := client.Cluster(path).Resource(schema.GroupVersionResource{
				Group:    "apis.kcp.io",
				Version:  "v1alpha1",
				Resource: "apibindings",
			}).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}

			parentPath, ok := path.Parent()
			if !ok {
				klog.ErrorS(kerrors.NewBadRequest("parent cluster path not found"), "path", path)
				return nil, kerrors.NewBadRequest("parent cluster path not found")
			}

			entityType := cfg.AccountEntityName
			if strings.HasSuffix(parentPath.String(), "orgs") {
				entityType = cfg.MainEntityName
			}

			klog.V(8).InfoS("using entity type", "entityType", entityType)

			err = apiBindings.EachListItem(func(o runtime.Object) error {
				binding := o.(*unstructured.Unstructured)

				apiExportName, ok, err := unstructured.NestedString(binding.Object, "spec", "reference", "export", "name")
				if err != nil || !ok {
					klog.ErrorS(err, "failed to get apiExportName from apibinding", "binding", binding.GetName())
					return err
				}

				apiExportWorkspacePath, ok, err := unstructured.NestedString(binding.Object, "status", "apiExportClusterName")
				if err != nil || !ok {
					klog.ErrorS(err, "failed to get apiExportWorkspacePath from apibinding", "binding", binding.GetName())
					return err
				}

				exportCtx := genericapirequest.WithCluster(ctx, genericapirequest.Cluster{
					Name: logicalcluster.Name(apiExportWorkspacePath),
				})

				exportOpts := options.DeepCopy()
				exportOpts.LabelSelector = labels.SelectorFromValidatedSet(map[string]string{
					cfg.ContentForLabel: apiExportName,
					cfg.EntityLabel:     entityType,
				})

				apiExportCCs, err := delegateLister.List(exportCtx, exportOpts)
				if kerrors.IsNotFound(err) {
					return nil
				}

				if err != nil {
					klog.ErrorS(err, "failed to list contentconfigurations from apiexport", "export", apiExportName, "workspace", apiExportWorkspacePath)
					return err
				}

				ul.Items = append(ul.Items, contentConfigurationWithResult(apiExportCCs.(*unstructured.UnstructuredList))...)

				return nil
			})
			if err != nil {
				return nil, err
			}

			providerCtx := genericapirequest.WithCluster(ctx, genericapirequest.Cluster{
				Name: logicalcluster.Name(providerWorkspaceID),
			})

			providerOpts := options.DeepCopy()
			providerOpts.LabelSelector = labels.SelectorFromValidatedSet(map[string]string{
				cfg.EntityLabel: entityType,
			})

			providerCCs, err := delegateLister.List(providerCtx, providerOpts)
			if err != nil {
				klog.ErrorS(err, "failed to list contentconfigurations from provider workspace", "workspace", providerWorkspaceID)
				return nil, err
			}

			ul.Items = append(ul.Items, contentConfigurationWithResult(providerCCs.(*unstructured.UnstructuredList))...)

			return ul, nil
		}
	})
}

func Marketplace(provider *apiexport.Provider, cfg config.ServiceConfig) forwardingregistry.StorageWrapper {
	return forwardingregistry.StorageWrapperFunc(func(resource schema.GroupResource, storage *forwardingregistry.StoreFuncs) {
		storage.ListerFunc = func(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
			cluster := genericapirequest.ClusterFrom(ctx)

			cl, err := provider.Get(ctx, multicluster.ClusterName(cluster.Name.String()))
			if err != nil {
				return nil, fmt.Errorf("failed to get cluster from provider: %w", err)
			}

			// Get APIBindings for this specific cluster
			installedAPIBindings := &apisv1alpha1.APIBindingList{}
			if err := cl.GetClient().List(ctx, installedAPIBindings); err != nil {
				return nil, fmt.Errorf("failed to list apibindings: %w", err)
			}

			lister := provider.Lister()

			var providerList extensionapiv1alpha1.ProviderMetadataList
			if err := lister.List(ctx, &providerList); err != nil {
				return nil, fmt.Errorf("failed to list providermetadatas: %w", err)
			}

			var results unstructured.UnstructuredList
			results.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind("MarketplaceEntryList"))

			// For each provider, find matching APIExports across all shards
			for _, provider := range providerList.Items {
				exportList := &apisv1alpha1.APIExportList{}

				if err := lister.List(ctx, exportList, &client.ListOptions{
					LabelSelector: labels.SelectorFromValidatedSet(map[string]string{
						cfg.ContentForLabel: provider.GetName(),
					}),
				}); err != nil {
					return nil, fmt.Errorf("failed to list apiexports for provider %s: %w", provider.GetName(), err)
				}

				for _, export := range exportList.Items {
					if len(export.Spec.LatestResourceSchemas) == 0 {
						continue
					}

					idx := slices.IndexFunc(installedAPIBindings.Items, func(item apisv1alpha1.APIBinding) bool {
						return item.Spec.Reference.Export.Name == export.Name &&
							item.Status.APIExportClusterName == export.Annotations["kcp.io/cluster"]
					})

					var apiBindingName string
					if idx != -1 {
						apiBindingName = installedAPIBindings.Items[idx].Name
					}

					provider.ManagedFields = nil // clear managed fields to declutter the output
					export.ManagedFields = nil

					unstructuredEntry, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&v1alpha1.MarketplaceEntry{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("%s-%s", export.Name, provider.Name), // TODO: we might need to fix the name length to not exceed the kubernetes limit
						},
						Spec: v1alpha1.MarketplaceEntrySpec{
							ProviderMetadata: *provider.DeepCopy(),
							APIExport:        *export.DeepCopy(),
							APIBindingName:   apiBindingName,
						},
					})
					if err != nil {
						return nil, fmt.Errorf("failed to convert marketplace entry to unstructured for export %s and provider %s: %w", export.Name, provider.Name, err)
					}

					us := unstructured.Unstructured{Object: unstructuredEntry}
					us.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind("MarketplaceEntry"))
					results.Items = append(results.Items, us)
				}
			}
			return &results, nil
		}
	})
}
