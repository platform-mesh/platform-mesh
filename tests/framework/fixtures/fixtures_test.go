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

package fixtures

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"

	kcpmanifests "go.platform-mesh.io/apis/static-assets/kcp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

const manifests = `
apiVersion: apis.kcp.io/v1alpha2
kind: APIExport
metadata:
  name: core.platform-mesh.io
spec:
  permissionClaims:
  - group: tenancy.kcp.io
    resource: workspaces
    identityHash: '{{ .data.apiExportRootTenancyKcpIoIdentityHash }}'
    verbs: null
  - group: core.kcp.io
    resource: logicalclusters
    identityHash: ""
    verbs: ["get"]
---
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
---
apiVersion: apis.kcp.io/v1alpha1
kind: APIResourceSchema
metadata:
  name: v1.accounts.core.platform-mesh.io
`

func TestDecodeSortPatch(t *testing.T) {
	objs := Decode(t, []byte(manifests))
	require.Len(t, objs, 3, "empty documents are skipped")

	sorted := sortForApply(objs)
	kinds := make([]string, 0, len(sorted))
	for _, obj := range sorted {
		kinds = append(kinds, obj.GetKind())
	}
	require.Equal(t, []string{"APIResourceSchema", "APIExport", "ConfigMap"}, kinds)

	ids := identities{}
	ids.add("tenancy.kcp.io", "workspaces", "tenancy-hash")
	ids.add("core.kcp.io", "shards", "shards-hash")
	export := objs[0]
	ids.patch(export)

	claims, _, err := unstructured.NestedSlice(export.Object, "spec", "permissionClaims")
	require.NoError(t, err)
	require.Equal(t, "tenancy-hash", claims[0].(map[string]any)["identityHash"])
	require.Equal(t, []any{"*"}, claims[0].(map[string]any)["verbs"])
	require.Empty(t, claims[1].(map[string]any)["identityHash"], "a claim on a built-in resource keeps its empty hash")
	require.Equal(t, []any{"get"}, claims[1].(map[string]any)["verbs"])
}

func TestWorkspaceTypesDecode(t *testing.T) {
	for _, file := range workspaceTypeFiles {
		raw, err := fs.ReadFile(kcpmanifests.Resources, file)
		require.NoError(t, err)
		objs := Decode(t, Render(t, raw, manifestData))
		require.Len(t, objs, 1, file)
		require.Equal(t, "WorkspaceType", objs[0].GetKind(), file)
		bindings, _, _ := unstructured.NestedSlice(objs[0].Object, "spec", "defaultAPIBindings")
		for _, b := range bindings {
			require.NotEqual(t, "terminal.platform-mesh.io", b.(map[string]any)["export"], "%s: the terminal feature is off", file)
		}
	}
}

func TestRenderMissingKey(t *testing.T) {
	out := Render(t, []byte("a: {{ .x }}\n{{- if eq .y \"true\" }}\nb: 1\n{{- end }}\n"), map[string]any{"x": "1", "y": "false"})
	require.Equal(t, "a: 1\n", string(out))
}

func TestBindingFor(t *testing.T) {
	export := &kcpapisv1alpha2.APIExport{
		ObjectMeta: metav1.ObjectMeta{Name: "core.platform-mesh.io"},
		Spec: kcpapisv1alpha2.APIExportSpec{
			PermissionClaims: []kcpapisv1alpha2.PermissionClaim{
				{GroupResource: kcpapisv1alpha2.GroupResource{Group: "tenancy.kcp.io", Resource: "workspaces"}, IdentityHash: "abc", Verbs: []string{"*"}},
				{
					GroupResource: kcpapisv1alpha2.GroupResource{Group: "", Resource: "secrets"},
					Verbs:         []string{"get"},
					DefaultSelector: &kcpapisv1alpha2.PermissionClaimSelector{LabelSelector: metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "core.platform-mesh.io/idp-name", Operator: metav1.LabelSelectorOpExists}},
					}},
				},
			},
		},
	}
	binding := BindingFor(export, logicalcluster.NewPath("root:platform-mesh-system"))

	require.Equal(t, "core.platform-mesh.io", binding.Name)
	require.Equal(t, &kcpapisv1alpha2.ExportBindingReference{Path: "root:platform-mesh-system", Name: "core.platform-mesh.io"}, binding.Spec.Reference.Export)
	require.Len(t, binding.Spec.PermissionClaims, 2)

	workspaces := binding.Spec.PermissionClaims[0]
	require.Equal(t, kcpapisv1alpha2.ClaimAccepted, workspaces.State)
	require.Equal(t, "abc", workspaces.IdentityHash, "the export's identity hash is kept")
	require.Equal(t, []string{"*"}, workspaces.Verbs)
	require.True(t, workspaces.Selector.MatchAll, "a claim without defaultSelector is accepted with matchAll")

	secrets := binding.Spec.PermissionClaims[1]
	require.False(t, secrets.Selector.MatchAll)
	require.Equal(t, export.Spec.PermissionClaims[1].DefaultSelector.MatchExpressions, secrets.Selector.MatchExpressions, "a defaultSelector is accepted as is")
}

func TestSplitExport(t *testing.T) {
	objs := Decode(t, []byte(`
apiVersion: apis.kcp.io/v1alpha2
kind: APIExport
metadata:
  name: core.platform-mesh.io
spec:
  resources:
  - group: core.platform-mesh.io
    name: accounts
    schema: v1.accounts.core.platform-mesh.io
    storage: {crd: {}}
  - group: core.platform-mesh.io
    name: stores
    schema: v1.stores.core.platform-mesh.io
    storage: {crd: {}}
`))
	out := splitExport(t, objs, exportSplit{name: "fga.platform-mesh.io", from: "core.platform-mesh.io", resources: []string{"stores"}})
	require.Len(t, out, 2)

	core, fga := out[0], out[1]
	names := func(obj *unstructured.Unstructured) []string {
		resources, _, _ := unstructured.NestedSlice(obj.Object, "spec", "resources")
		names := make([]string, 0, len(resources))
		for _, r := range resources {
			names = append(names, r.(map[string]any)["name"].(string))
		}
		return names
	}
	require.Equal(t, []string{"accounts"}, names(core), "the moved resource leaves the source export")
	require.Equal(t, "fga.platform-mesh.io", fga.GetName())
	require.Equal(t, "APIExport", fga.GetKind())
	require.Equal(t, []string{"stores"}, names(fga))
	secret, _, _ := unstructured.NestedString(fga.Object, "spec", "identity", "secretRef", "name")
	require.Equal(t, "core.platform-mesh.io", secret, "the new export shares the source's identity")
}
