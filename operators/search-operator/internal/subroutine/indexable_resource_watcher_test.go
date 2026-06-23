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

package subroutine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	accountv1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"go.platform-mesh.io/apis/search/v1alpha1"
	"go.platform-mesh.io/search-operator/internal/opensearch"
)

func TestBuildPayloadSeparatesRawJSONFromText(t *testing.T) {
	resource := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "core.platform-mesh.io/v1alpha1",
			"kind":       "Component",
			"metadata": map[string]any{
				"name":          "my-component",
				"namespace":     "default",
				"uid":           "abc-123-def",
				"managedFields": []any{map[string]any{"manager": "kubectl"}},
				"labels": map[string]any{
					"app": "frontend",
				},
			},
			"spec": map[string]any{
				"replicas": float64(3),
				"image":    "nginx:latest",
				"enabled":  true,
			},
		},
	}

	rawJSON, text, err := buildPayload(resource)
	if err != nil {
		t.Fatalf("buildPayload returned error: %v", err)
	}

	// rawJSON must be valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &parsed); err != nil {
		t.Fatalf("rawJSON is not valid JSON: %v", err)
	}

	// rawJSON should NOT contain managedFields
	if strings.Contains(rawJSON, "managedFields") {
		t.Fatal("rawJSON should not contain managedFields")
	}

	// text should be YAML (contains colons and indentation, no braces for the whole object)
	if !strings.Contains(text, "kind: Component") {
		t.Error("text should contain 'kind: Component'")
	}
	if !strings.Contains(text, "replicas: 3") {
		t.Error("text should contain 'replicas: 3'")
	}
	if !strings.Contains(text, "image: nginx:latest") {
		t.Error("text should contain 'image: nginx:latest'")
	}

	// text should NOT contain managedFields
	if strings.Contains(text, "managedFields") {
		t.Fatal("text should not contain managedFields")
	}
}

func TestBuildFGAObjectName(t *testing.T) {
	tests := []struct {
		name      string
		group     string
		kind      string
		clusterID string
		resource  string
		namespace string
		want      string
	}{
		{
			name:      "namespaced resource",
			group:     "core.platform-mesh.io",
			kind:      "Component",
			clusterID: "cluster1",
			resource:  "comp1",
			namespace: "ns1",
			want:      "core_platform-mesh_io_component:cluster1/ns1/comp1",
		},
		{
			name:      "cluster scoped resource",
			group:     "core.platform-mesh.io",
			kind:      "Account",
			clusterID: "cluster1",
			resource:  "acc1",
			namespace: "",
			want:      "core_platform-mesh_io_account:cluster1/acc1",
		},
		{
			name:      "core resource",
			group:     "",
			kind:      "Namespace",
			clusterID: "cluster1",
			resource:  "ns1",
			namespace: "",
			want:      "core_namespace:cluster1/ns1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildFGAObjectName(tt.group, tt.kind, tt.clusterID, tt.resource, tt.namespace); got != tt.want {
				t.Errorf("buildFGAObjectName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapResourceToFGAObject(t *testing.T) {
	accountInfo := &accountv1alpha1.AccountInfo{
		Spec: accountv1alpha1.AccountInfoSpec{
			Account: accountv1alpha1.AccountLocation{
				Name:            "teams",
				OriginClusterId: "account-origin",
			},
			Organization: accountv1alpha1.AccountLocation{
				Name:            "sap",
				OriginClusterId: "org-origin",
			},
		},
	}

	tests := []struct {
		name        string
		group       string
		kind        string
		clusterID   multicluster.ClusterName
		accountInfo *accountv1alpha1.AccountInfo
		wantGroup   string
		wantKind    string
		wantCluster multicluster.ClusterName
	}{
		{
			name:        "account maps to search account using OriginClusterId",
			group:       v1alpha1.GroupName,
			kind:        v1alpha1.AccountKind,
			clusterID:   "acc-cluster",
			accountInfo: accountInfo,
			wantGroup:   v1alpha1.GroupName,
			wantKind:    v1alpha1.AccountKind,
			wantCluster: "account-origin",
		},
		{
			name:        "workspace maps to search account using OriginClusterId",
			group:       "tenancy.kcp.io",
			kind:        "Workspace",
			clusterID:   "ws-cluster",
			accountInfo: accountInfo,
			wantGroup:   v1alpha1.GroupName,
			wantKind:    v1alpha1.AccountKind,
			wantCluster: "account-origin",
		},
		{
			name:        "organization maps to search account preserving origin cluster id",
			group:       v1alpha1.GroupName,
			kind:        v1alpha1.OrganizationKind,
			clusterID:   "org-resource-cluster",
			accountInfo: accountInfo,
			wantGroup:   v1alpha1.GroupName,
			wantKind:    v1alpha1.AccountKind,
			wantCluster: "org-origin",
		},
		{
			name:        "unmapped resource keeps own type",
			group:       "core.platform-mesh.io",
			kind:        "Component",
			clusterID:   "component-cluster",
			accountInfo: accountInfo,
			wantGroup:   "core.platform-mesh.io",
			wantKind:    "Component",
			wantCluster: "component-cluster",
		},
		{
			name:        "account-like resource without accountInfo falls back to clusterID",
			group:       v1alpha1.GroupName,
			kind:        v1alpha1.AccountKind,
			clusterID:   "acc-cluster",
			accountInfo: nil,
			wantGroup:   v1alpha1.GroupName,
			wantKind:    v1alpha1.AccountKind,
			wantCluster: "acc-cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotGroup, gotKind, gotCluster := mapResourceToFGAObject(tt.group, tt.kind, tt.clusterID, tt.accountInfo)
			if gotGroup != tt.wantGroup || gotKind != tt.wantKind || gotCluster != tt.wantCluster {
				t.Fatalf(
					"mapResourceToFGAObject() = (%s, %s, %s), want (%s, %s, %s)",
					gotGroup, gotKind, gotCluster,
					tt.wantGroup, tt.wantKind, tt.wantCluster,
				)
			}
		})
	}
}

func TestResolveResourceClusterID(t *testing.T) {
	resourceWithAnnotation := &unstructured.Unstructured{}
	resourceWithAnnotation.SetAnnotations(map[string]string{
		kcpClusterAnnotation: "ann-cluster",
	})

	resourceWithoutAnnotation := &unstructured.Unstructured{}

	if got := resolveResourceClusterID(resourceWithAnnotation, "fallback"); got != "ann-cluster" {
		t.Fatalf("resolveResourceClusterID() with annotation = %q, want %q", got, "ann-cluster")
	}

	if got := resolveResourceClusterID(resourceWithoutAnnotation, "fallback"); got != "fallback" {
		t.Fatalf("resolveResourceClusterID() without annotation = %q, want %q", got, "fallback")
	}
}

func TestResolveSpecClusterID(t *testing.T) {
	resource := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"cluster": " spec-cluster ",
			},
		},
	}

	if got := resolveSpecClusterID(resource); got != "spec-cluster" {
		t.Fatalf("resolveSpecClusterID() = %q, want %q", got, "spec-cluster")
	}

	resourceNoSpec := &unstructured.Unstructured{
		Object: map[string]any{},
	}
	if got := resolveSpecClusterID(resourceNoSpec); got != "" {
		t.Fatalf("resolveSpecClusterID() without spec = %q, want empty", got)
	}
}

func TestResolveAccountInfoLookupClusters(t *testing.T) {
	resource := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"cluster": "spec-cluster",
			},
		},
	}

	got := resolveAccountInfoLookupClusters(resource, "ctx-cluster", "resource-cluster")
	want := []multicluster.ClusterName{"resource-cluster", "ctx-cluster", "spec-cluster"}
	if len(got) != len(want) {
		t.Fatalf("resolveAccountInfoLookupClusters() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolveAccountInfoLookupClusters()[%d] = %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}

	resourceDup := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"cluster": "ctx-cluster",
			},
		},
	}

	gotDup := resolveAccountInfoLookupClusters(resourceDup, "ctx-cluster", "ctx-cluster")
	if len(gotDup) != 1 || gotDup[0] != "ctx-cluster" {
		t.Fatalf("resolveAccountInfoLookupClusters() dedupe = %v, want [ctx-cluster]", gotDup)
	}
}

func TestExtractConfiguredFieldsSupportsNestedPaths(t *testing.T) {
	resource := &unstructured.Unstructured{
		Object: map[string]any{
			"description": "top-level description",
			"spec": map[string]any{
				"summary": "nested summary",
			},
		},
	}

	got := extractConfiguredFields(resource, []string{"description", "spec.summary", "spec.missing"})
	if len(got) != 2 {
		t.Fatalf("extractConfiguredFields() len = %d, want 2 (%v)", len(got), got)
	}
	if got["description"] != "top-level description" {
		t.Fatalf("description = %v, want top-level description", got["description"])
	}
	if got["spec.summary"] != "nested summary" {
		t.Fatalf("spec.summary = %v, want nested summary", got["spec.summary"])
	}
}

func TestBuildDocumentSourceAddsConfiguredFields(t *testing.T) {
	doc := &opensearch.ResourceDocument{
		ID:            "doc-1",
		Kind:          "Component",
		Name:          "component-a",
		ClusterName:   "root:orgs:sap",
		WorkspacePath: "root:orgs:sap:team-a",
		UpdatedAt:     time.Unix(0, 0).UTC(),
	}

	source, err := buildDocumentSource(doc, map[string]any{
		"description":  "top-level description",
		"spec.summary": "nested summary",
	})
	if err != nil {
		t.Fatalf("buildDocumentSource() returned error: %v", err)
	}

	if got := source["description"]; got != "top-level description" {
		t.Fatalf("description = %v, want top-level description", got)
	}

	spec, ok := source["spec"].(map[string]any)
	if !ok {
		t.Fatalf("spec = %T, want map[string]any", source["spec"])
	}
	if got := spec["summary"]; got != "nested summary" {
		t.Fatalf("spec.summary = %v, want nested summary", got)
	}
}
