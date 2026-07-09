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

package kcp

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	pmsearchv1alpha1 "go.platform-mesh.io/apis/search/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"go.platform-mesh.io/search-service/internal/config"
	"go.platform-mesh.io/search-service/internal/service/search"
)

type SearchIndexResolver struct {
	searchIndexClient    dynamic.Interface
	orgSearchIndexClient dynamic.Interface
	orgClient            dynamic.Interface
	cfg                  config.SearchIndexConfig
	log                  *logger.Logger
}

const searchIndexOrgClusterIDLabel = "search.platform-mesh.io/org-cluster-id"

func NewSearchIndexResolver(restCfg *rest.Config, cfg config.SearchIndexConfig, log *logger.Logger) (*SearchIndexResolver, error) {
	searchIndexCfg, err := configForKCPCluster(cfg.WorkspacePath, restCfg)
	if err != nil {
		return nil, fmt.Errorf("create SearchIndex kcp workspace config: %w", err)
	}

	searchIndexClient, err := dynamic.NewForConfig(searchIndexCfg)
	if err != nil {
		return nil, fmt.Errorf("create SearchIndex kcp dynamic client: %w", err)
	}

	orgWorkspacePath := strings.TrimSpace(cfg.OrgWorkspacePath)
	if orgWorkspacePath == "" {
		orgWorkspacePath = "root:orgs"
		cfg.OrgWorkspacePath = orgWorkspacePath
	}
	orgCfg, err := configForKCPCluster(orgWorkspacePath, restCfg)
	if err != nil {
		return nil, fmt.Errorf("create org kcp workspace config: %w", err)
	}

	orgClient, err := dynamic.NewForConfig(orgCfg)
	if err != nil {
		return nil, fmt.Errorf("create org kcp dynamic client: %w", err)
	}

	orgSearchIndexClient, err := dynamic.NewForConfig(orgCfg)
	if err != nil {
		return nil, fmt.Errorf("create org SearchIndex dynamic client: %w", err)
	}

	return &SearchIndexResolver{
		searchIndexClient:    searchIndexClient,
		orgSearchIndexClient: orgSearchIndexClient,
		orgClient:            orgClient,
		cfg:                  cfg,
		log:                  log,
	}, nil
}

func (s *SearchIndexResolver) ResolveIndex(ctx context.Context, org, resource string) (search.SearchIndexRef, error) {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		return search.SearchIndexRef{}, fmt.Errorf("resource is required")
	}

	indices, err := s.ListIndices(ctx, org)
	if err != nil {
		return search.SearchIndexRef{}, err
	}

	normalized := normalizeName(resource)
	for _, index := range indices {
		if normalizeName(index.Resource) == normalized {
			return index, nil
		}
	}

	return search.SearchIndexRef{}, fmt.Errorf("no SearchIndex matched org %q and resource %q", org, resource)
}

func (s *SearchIndexResolver) ListIndices(ctx context.Context, org string) ([]search.SearchIndexRef, error) {
	org = strings.TrimSpace(org)
	if org == "" {
		return nil, fmt.Errorf("organization is required")
	}

	orgClusterID, err := s.resolveOrganizationClusterID(ctx, org)
	if err != nil {
		return nil, err
	}

	list, err := s.listSearchIndicesForOrg(ctx, orgClusterID)
	if err != nil {
		return nil, fmt.Errorf("list SearchIndex resources: %w", err)
	}
	if len(list.Items) == 0 {
		return nil, fmt.Errorf("no SearchIndex resources found in workspaces %q or %q", s.cfg.WorkspacePath, s.cfg.OrgWorkspacePath)
	}

	refs := make([]search.SearchIndexRef, 0, len(list.Items))
	seenResource := make(map[string]string, len(list.Items))
	for _, item := range list.Items {
		ref, ok := mapSearchIndexRef(item, orgClusterID, s.cfg)
		if !ok {
			continue
		}

		if existingIndex, exists := seenResource[ref.Resource]; exists && existingIndex != ref.IndexName {
			return nil, fmt.Errorf("multiple SearchIndex resources match org %q and resource %q", org, ref.Resource)
		}
		seenResource[ref.Resource] = ref.IndexName
		refs = append(refs, ref)
	}

	if len(refs) == 0 {
		return nil, fmt.Errorf("no active SearchIndex resources found for org %q", org)
	}

	slices.SortFunc(refs, func(a, b search.SearchIndexRef) int {
		return cmp.Compare(a.Resource, b.Resource)
	})

	return refs, nil
}

func (s *SearchIndexResolver) resolveOrganizationClusterID(ctx context.Context, org string) (string, error) {
	obj, err := s.orgClient.Resource(workspaceGVR).Get(ctx, org, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("resolve workspace cluster ID for org %q in workspace %q: %w", org, s.cfg.OrgWorkspacePath, err)
	}

	clusterID, found, err := unstructured.NestedString(obj.Object, "spec", "cluster")
	if err != nil || !found {
		return "", fmt.Errorf("workspace %q does not expose spec.cluster", org)
	}

	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return "", fmt.Errorf("workspace %q does not expose spec.cluster", org)
	}
	return clusterID, nil
}

func (s *SearchIndexResolver) listSearchIndicesForOrg(ctx context.Context, orgClusterID string) (pmsearchv1alpha1.SearchIndexList, error) {
	list, err := listSearchIndices(ctx, s.searchIndexClient, s.cfg, orgClusterID)
	if err != nil {
		return list, err
	}
	if len(list.Items) > 0 {
		return list, nil
	}

	list, err = listSearchIndices(ctx, s.searchIndexClient, s.cfg, "")
	if err != nil {
		return list, fmt.Errorf("fallback: %w", err)
	}
	if len(list.Items) > 0 {
		return list, nil
	}

	if s.cfg.OrgWorkspacePath != s.cfg.WorkspacePath {
		orgList, orgErr := listSearchIndices(ctx, s.orgSearchIndexClient, s.cfg, orgClusterID)
		if orgErr != nil {
			return orgList, orgErr
		}
		if len(orgList.Items) > 0 {
			return orgList, nil
		}
	}

	if s.cfg.OrgWorkspacePath != s.cfg.WorkspacePath {
		orgList, orgErr := listSearchIndices(ctx, s.orgSearchIndexClient, s.cfg, "")
		if orgErr != nil {
			return orgList, fmt.Errorf("fallback: %w", orgErr)
		}
		if len(orgList.Items) > 0 {
			return orgList, nil
		}
	}

	return list, nil
}

func listSearchIndices(ctx context.Context, client dynamic.Interface, cfg config.SearchIndexConfig, orgClusterID string) (pmsearchv1alpha1.SearchIndexList, error) {
	var list pmsearchv1alpha1.SearchIndexList

	listOpts := metav1.ListOptions{}
	if orgClusterID != "" {
		listOpts.LabelSelector = fmt.Sprintf("%s=%s", searchIndexOrgClusterIDLabel, orgClusterID)
	}

	objList, err := client.Resource(searchIndexGVR(cfg)).List(ctx, listOpts)
	if err != nil {
		return list, err
	}

	items := make([]pmsearchv1alpha1.SearchIndex, 0, len(objList.Items))
	for _, item := range objList.Items {
		searchIndex := pmsearchv1alpha1.SearchIndex{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &searchIndex); err != nil {
			return list, fmt.Errorf("decode SearchIndex %q: %w", item.GetName(), err)
		}
		items = append(items, searchIndex)
	}

	list.Items = items
	return list, nil
}

func mapSearchIndexRef(item pmsearchv1alpha1.SearchIndex, orgClusterID string, cfg config.SearchIndexConfig) (search.SearchIndexRef, bool) {
	indexName := strings.TrimSpace(item.Status.IndexName)
	if indexName == "" {
		indexName = strings.TrimSpace(item.Name)
	}
	if indexName == "" {
		return search.SearchIndexRef{}, false
	}

	orgID := firstNonEmpty(strings.TrimSpace(item.Spec.OrganizationClusterID), orgClusterID)
	resource := deriveResourceName(item.Name, item.Spec.IndexPrefix, orgID)
	if resource == "" {
		resource = deriveResourceName(indexName, item.Spec.IndexPrefix, orgID)
	}
	if resource == "" {
		return search.SearchIndexRef{}, false
	}

	return search.SearchIndexRef{
		Resource:              resource,
		IndexName:             indexName,
		IndexPrefix:           strings.TrimSpace(item.Spec.IndexPrefix),
		OrganizationClusterID: orgID,
		DefaultFields:         normalizeStringSlice(item.Spec.DefaultFields),
		FilterableFields:      normalizeStringSlice(item.Spec.FilterableFields),
		SemanticFields:        normalizeStringSlice(item.Spec.SemanticFields),
		Group:                 cfg.Group,
		Version:               cfg.Version,
	}, true
}

func deriveResourceName(name, indexPrefix, orgClusterID string) string {
	name = normalizeName(name)
	if name == "" {
		return ""
	}

	prefix := normalizeName(indexPrefix)
	orgID := normalizeName(orgClusterID)

	trimmed := name
	if prefix != "" {
		pattern := prefix + "-"
		if !strings.HasPrefix(trimmed, pattern) {
			return ""
		}
		trimmed = strings.TrimPrefix(trimmed, pattern)
	}
	if orgID != "" {
		pattern := orgID + "-"
		if !strings.HasPrefix(trimmed, pattern) {
			return ""
		}
		trimmed = strings.TrimPrefix(trimmed, pattern)
	}

	return strings.Trim(trimmed, "-")
}

func normalizeName(value string) string {
	value = strings.ToLower(value)

	var b strings.Builder
	b.Grow(len(value))
	lastWasDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastWasDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasDash = false
		default:
			if !lastWasDash {
				b.WriteByte('-')
				lastWasDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}

	slices.Sort(out)

	return out
}

func configForKCPCluster(clusterName string, cfg *rest.Config) (*rest.Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	clusterCfg := rest.CopyConfig(cfg)
	clusterCfgURL, err := url.Parse(clusterCfg.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse host URL: %w", err)
	}

	clusterCfgURL.Path = fmt.Sprintf("/clusters/%s", clusterName)
	clusterCfg.Host = clusterCfgURL.String()

	return clusterCfg, nil
}

func searchIndexGVR(cfg config.SearchIndexConfig) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    cfg.Group,
		Version:  cfg.Version,
		Resource: cfg.Resource,
	}
}

var workspaceGVR = schema.GroupVersionResource{
	Group:    "tenancy.kcp.io",
	Version:  "v1alpha1",
	Resource: "workspaces",
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
