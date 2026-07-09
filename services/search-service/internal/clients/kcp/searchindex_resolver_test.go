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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.platform-mesh.io/search-service/internal/config"

	"k8s.io/client-go/rest"
)

func TestListIndicesBuildsResourceDescriptors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/clusters/root:orgs/apis/tenancy.kcp.io/v1alpha1/workspaces/acme":
			writeJSON(t, w, workspacePayload("acme", "cluster-123"))
		case "/clusters/root:providers:search/apis/search.platform-mesh.io/v1alpha1/searchindexes":
			writeJSON(t, w, searchIndexListPayload(map[string]any{
				"items": []map[string]any{
					{
						"metadata": map[string]any{"name": "pm-cluster-123-accounts"},
						"spec": map[string]any{
							"indexPrefix":           "pm",
							"organizationClusterID": "cluster-123",
							"defaultFields":         []string{"displayName", "description"},
							"filterableFields":      []string{"status"},
							"semanticFields":        []string{"description"},
						},
						"status": map[string]any{"indexName": "pm-cluster-123-accounts"},
					},
					{
						"metadata": map[string]any{"name": "pm-cluster-123-products"},
						"spec": map[string]any{
							"indexPrefix":           "pm",
							"organizationClusterID": "cluster-123",
							"defaultFields":         []string{"name"},
							"filterableFields":      []string{"category"},
						},
						"status": map[string]any{"indexName": "pm-cluster-123-products"},
					},
				},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	resolver := newTestResolver(t, server.URL)
	refs, err := resolver.ListIndices(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ListIndices returned error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	if refs[0].Resource != "accounts" || refs[1].Resource != "products" {
		t.Fatalf("unexpected resources: %+v", refs)
	}
}

func TestResolveIndexByResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/clusters/root:orgs/apis/tenancy.kcp.io/v1alpha1/workspaces/acme":
			writeJSON(t, w, workspacePayload("acme", "cluster-123"))
		case "/clusters/root:providers:search/apis/search.platform-mesh.io/v1alpha1/searchindexes":
			writeJSON(t, w, searchIndexListPayload(map[string]any{
				"items": []map[string]any{
					{
						"metadata": map[string]any{"name": "pm-cluster-123-accounts"},
						"spec": map[string]any{
							"indexPrefix":           "pm",
							"organizationClusterID": "cluster-123",
						},
						"status": map[string]any{"indexName": "pm-cluster-123-accounts"},
					},
				},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	resolver := newTestResolver(t, server.URL)
	ref, err := resolver.ResolveIndex(context.Background(), "acme", "accounts")
	if err != nil {
		t.Fatalf("ResolveIndex returned error: %v", err)
	}
	if ref.IndexName != "pm-cluster-123-accounts" {
		t.Fatalf("unexpected index name: %s", ref.IndexName)
	}
	if ref.Resource != "accounts" {
		t.Fatalf("unexpected resource: %s", ref.Resource)
	}
}

func TestListIndicesFallsBackToOrgWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/clusters/root:orgs/apis/tenancy.kcp.io/v1alpha1/workspaces/acme":
			writeJSON(t, w, workspacePayload("acme", "cluster-123"))
		case "/clusters/root:providers:search/apis/search.platform-mesh.io/v1alpha1/searchindexes":
			writeJSON(t, w, searchIndexListPayload(map[string]any{"items": []map[string]any{}}))
		case "/clusters/root:orgs/apis/search.platform-mesh.io/v1alpha1/searchindexes":
			writeJSON(t, w, searchIndexListPayload(map[string]any{
				"items": []map[string]any{
					{
						"metadata": map[string]any{"name": "pm-orgs-cluster-123-components"},
						"spec": map[string]any{
							"indexPrefix":           "pm-orgs",
							"organizationClusterID": "cluster-123",
						},
						"status": map[string]any{"indexName": "pm-orgs-cluster-123-components"},
					},
				},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	resolver := newTestResolver(t, server.URL)
	refs, err := resolver.ListIndices(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ListIndices returned error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].Resource != "components" {
		t.Fatalf("unexpected resource: %s", refs[0].Resource)
	}
}

func TestListIndicesReturnsErrorWhenResourceIsAmbiguous(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/clusters/root:orgs/apis/tenancy.kcp.io/v1alpha1/workspaces/acme":
			writeJSON(t, w, workspacePayload("acme", "cluster-123"))
		case "/clusters/root:providers:search/apis/search.platform-mesh.io/v1alpha1/searchindexes":
			writeJSON(t, w, searchIndexListPayload(map[string]any{
				"items": []map[string]any{
					{
						"metadata": map[string]any{"name": "pm-cluster-123-accounts"},
						"spec": map[string]any{
							"indexPrefix":           "pm",
							"organizationClusterID": "cluster-123",
						},
						"status": map[string]any{"indexName": "pm-cluster-123-accounts-a"},
					},
					{
						"metadata": map[string]any{"name": "pm-cluster-123-accounts"},
						"spec": map[string]any{
							"indexPrefix":           "pm",
							"organizationClusterID": "cluster-123",
						},
						"status": map[string]any{"indexName": "pm-cluster-123-accounts-b"},
					},
				},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	resolver := newTestResolver(t, server.URL)
	if _, err := resolver.ListIndices(context.Background(), "acme"); err == nil {
		t.Fatalf("expected ambiguous resource error")
	}
}

func newTestResolver(t *testing.T, base string) *SearchIndexResolver {
	t.Helper()

	resolver, err := NewSearchIndexResolver(&rest.Config{Host: base}, config.SearchIndexConfig{
		WorkspacePath:    "root:providers:search",
		OrgWorkspacePath: "root:orgs",
		Group:            "search.platform-mesh.io",
		Version:          "v1alpha1",
		Resource:         "searchindexes",
	}, nil)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}

	return resolver
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}

func workspacePayload(name, clusterID string) map[string]any {
	return map[string]any{
		"apiVersion": "tenancy.kcp.io/v1alpha1",
		"kind":       "Workspace",
		"metadata": map[string]any{
			"name": name,
		},
		"spec": map[string]any{
			"cluster": clusterID,
		},
	}
}

func searchIndexListPayload(payload map[string]any) map[string]any {
	payload["apiVersion"] = "search.platform-mesh.io/v1alpha1"
	payload["kind"] = "SearchIndexList"
	return payload
}
