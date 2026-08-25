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
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	pmmarketplacev1alpha1 "go.platform-mesh.io/apis/marketplace/v1alpha1"
	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/virtual-workspaces/pkg/config"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog/v2"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/client-go/dynamic"
	"github.com/kcp-dev/logicalcluster/v3"
	mcpcache "github.com/kcp-dev/multicluster-provider/pkg/cache"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/forwardingregistry"
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

func Marketplace(
	lister mcpcache.Lister,
	clusterClient func(context.Context, string) (ctrlruntimeclient.Client, error),
	cfg config.ServiceConfig,
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

					legacyExport := &kcpapisv1alpha1.APIExport{}
					if err := kcpapisv1alpha2.Convert_v1alpha2_APIExport_To_v1alpha1_APIExport(&export, legacyExport, nil); err != nil {
						return nil, fmt.Errorf("failed to convert APIExport %s to v1alpha1: %w", export.Name, err)
					}
					legacyOrigins, err := legacyPermissionClaimOrigins(&export)
					if err != nil {
						return nil, fmt.Errorf("failed to decode original v1alpha1 permission claims for APIExport %s: %w", export.Name, err)
					}
					if len(legacyExport.Spec.LatestResourceSchemas) == 0 {
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
					legacyExport.ManagedFields = nil
					legacyClaims, permissionClaims := projectPermissionClaims(export.Spec.PermissionClaims, legacyOrigins)
					legacyAPIExport := projectLegacyAPIExport(legacyExport, legacyClaims)

					unstructuredEntry, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pmmarketplacev1alpha1.MarketplaceEntry{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("%s-%s", export.Name, provider.Name), // TODO: we might need to fix the name length to not exceed the kubernetes limit
						},
						Spec: pmmarketplacev1alpha1.MarketplaceEntrySpec{
							ProviderMetadata:          *provider.DeepCopy(),
							APIExport:                 legacyAPIExport,
							APIExportPermissionClaims: permissionClaims,
							APIBindingName:            apiBindingName,
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

func projectLegacyAPIExport(
	export *kcpapisv1alpha1.APIExport,
	permissionClaims []pmmarketplacev1alpha1.LegacyPermissionClaim,
) pmmarketplacev1alpha1.LegacyAPIExport {
	return pmmarketplacev1alpha1.LegacyAPIExport{
		APIVersion: kcpapisv1alpha1.SchemeGroupVersion.String(),
		Kind:       "APIExport",
		Metadata:   *export.ObjectMeta.DeepCopy(),
		Spec: pmmarketplacev1alpha1.LegacyAPIExportSpec{
			LatestResourceSchemas:   append([]string(nil), export.Spec.LatestResourceSchemas...),
			Identity:                export.Spec.Identity.DeepCopy(),
			MaximalPermissionPolicy: export.Spec.MaximalPermissionPolicy.DeepCopy(),
			PermissionClaims:        permissionClaims,
		},
		Status: *export.Status.DeepCopy(),
	}
}

type permissionClaimKey struct {
	group        string
	resource     string
	identityHash string
}

type legacyPermissionClaimOrigin struct {
	claim kcpapisv1alpha1.PermissionClaim
	count int
}

func legacyPermissionClaimOrigins(export *kcpapisv1alpha2.APIExport) (map[permissionClaimKey]legacyPermissionClaimOrigin, error) {
	encoded, ok := export.Annotations[kcpapisv1alpha2.PermissionClaimsV1Alpha1Annotation]
	if !ok {
		return nil, nil
	}

	var claims []kcpapisv1alpha1.PermissionClaim
	if err := json.Unmarshal([]byte(encoded), &claims); err != nil {
		return nil, err
	}

	origins := make(map[permissionClaimKey]legacyPermissionClaimOrigin, len(claims))
	for i := range claims {
		key := permissionClaimKey{
			group:        claims[i].Group,
			resource:     claims[i].Resource,
			identityHash: claims[i].IdentityHash,
		}
		origin := origins[key]
		origin.claim = *claims[i].DeepCopy()
		origin.count++
		origins[key] = origin
	}

	return origins, nil
}

func projectPermissionClaims(
	authoritativeClaims []kcpapisv1alpha2.PermissionClaim,
	legacyOrigins map[permissionClaimKey]legacyPermissionClaimOrigin,
) ([]pmmarketplacev1alpha1.LegacyPermissionClaim, []kcpapisv1alpha2.PermissionClaim) {
	claimCounts := make(map[permissionClaimKey]int, len(authoritativeClaims))
	for _, claim := range authoritativeClaims {
		claimCounts[permissionClaimKey{
			group:        claim.Group,
			resource:     claim.Resource,
			identityHash: claim.IdentityHash,
		}]++
	}

	legacyClaims := make([]pmmarketplacev1alpha1.LegacyPermissionClaim, 0, len(authoritativeClaims))
	exactClaims := make([]kcpapisv1alpha2.PermissionClaim, 0, len(authoritativeClaims))
	for i := range authoritativeClaims {
		claim := &authoritativeClaims[i]
		key := permissionClaimKey{
			group:        claim.Group,
			resource:     claim.Resource,
			identityHash: claim.IdentityHash,
		}
		if claimCounts[key] != 1 {
			continue
		}

		origin, hasLegacyOrigin := legacyOrigins[key]
		if hasLegacyOrigin {
			// A v1alpha1 resourceSelector is name/namespace based and cannot be
			// translated to a v1alpha2 label selector. Neither current Marketplace
			// surface can carry it without an old or new client broadening it to
			// matchAll, so omit ambiguous or selector-only legacy claims entirely.
			if origin.count != 1 || !origin.claim.All || len(origin.claim.ResourceSelector) != 0 {
				continue
			}
		}

		claimCopy := kcpapisv1alpha2.PermissionClaim{}
		claim.DeepCopyInto(&claimCopy)
		exactClaims = append(exactClaims, claimCopy)

		if !isLegacyRepresentable(*claim) {
			continue
		}
		legacyClaims = append(legacyClaims, pmmarketplacev1alpha1.LegacyPermissionClaim{
			Group:        claim.Group,
			Resource:     claim.Resource,
			All:          true,
			IdentityHash: claim.IdentityHash,
		})
	}

	return legacyClaims, exactClaims
}

func isLegacyRepresentable(claim kcpapisv1alpha2.PermissionClaim) bool {
	if len(claim.Verbs) != 1 || claim.Verbs[0] != "*" {
		return false
	}
	if claim.DefaultSelector == nil {
		return true
	}

	return claim.DefaultSelector.MatchAll &&
		len(claim.DefaultSelector.MatchLabels) == 0 &&
		len(claim.DefaultSelector.MatchExpressions) == 0
}
