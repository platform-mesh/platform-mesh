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

package contentconfiguration

import (
	"context"
	"strings"

	"go.platform-mesh.io/extension-manager-operator/virtual-workspace/pkg/config"
	"go.platform-mesh.io/platform-mesh/libs/virtual-workspaces/storage"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog/v2"

	"github.com/kcp-dev/client-go/dynamic"
	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/forwardingregistry"
)

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

// Lookup wraps the ContentConfiguration storage so that a list in a workspace
// returns the ContentConfigurations that apply there: its own, those its
// APIBindings project from their export workspaces, and the provider's.
func Lookup(client dynamic.ClusterInterface, cfg config.Config, providerWorkspaceID string) forwardingregistry.StorageWrapper {
	return forwardingregistry.StorageWrapperFunc(func(resource schema.GroupResource, store *forwardingregistry.StoreFuncs) {
		delegateLister := store.ListerFunc
		store.ListerFunc = func(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
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

			path, ok := storage.ClusterPathFrom(ctx)
			if !ok {
				klog.Error("cluster path not found in context")
				return nil, apierrors.NewBadRequest("cluster path not found in context")
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
				klog.ErrorS(apierrors.NewBadRequest("parent cluster path not found"), "path", path)
				return nil, apierrors.NewBadRequest("parent cluster path not found")
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
				if apierrors.IsNotFound(err) {
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
