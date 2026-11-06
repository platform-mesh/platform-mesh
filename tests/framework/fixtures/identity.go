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
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// identities maps the resources of kcp's own exports (tenancy.kcp.io,
// topology.kcp.io, ...) to their identity hash, keyed by "<resource>.<group>",
// so manifests written with a placeholder or empty identityHash can be
// applied to any kcp. Matching is by resource, not group: core.kcp.io, for
// example, has exported (shards) and built-in (logicalclusters) resources.
type identities map[string]string

func (ids identities) add(group, resource, hash string) {
	if hash != "" {
		ids[resource+"."+group] = hash
	}
}

// patch fills spec.permissionClaims[].identityHash of an APIExport or
// APIBinding for every claim on a resource ids knows (claims on built-in
// types, apis.kcp.io and core.kcp.io keep theirs) and defaults missing verbs
// to "*". Other objects are left alone.
func (ids identities) patch(obj *unstructured.Unstructured) {
	if obj.GroupVersionKind().Group != apisGroup || (obj.GetKind() != "APIExport" && obj.GetKind() != "APIBinding") {
		return
	}
	claims, found, err := unstructured.NestedSlice(obj.Object, "spec", "permissionClaims")
	if !found || err != nil {
		return
	}
	for i := range claims {
		claim, ok := claims[i].(map[string]any)
		if !ok {
			continue
		}
		group, _ := claim["group"].(string)
		resource, _ := claim["resource"].(string)
		if hash, ok := ids[resource+"."+group]; ok {
			claim["identityHash"] = hash
		}
		// apigen writes `verbs: null` into config/resources, which kcp
		// rejects; the deployed manifests (platform-mesh-operator
		// manifests/kcp) claim all verbs.
		if claim["verbs"] == nil {
			claim["verbs"] = []any{"*"}
		}
	}
	_ = unstructured.SetNestedSlice(obj.Object, claims, "spec", "permissionClaims")
}

// rootIdentities reads the identity hashes of the APIExports in :root.
func rootIdentities(t testing.TB, root ctrlruntimeclient.Client) identities {
	t.Helper()
	exports := &unstructured.UnstructuredList{}
	exports.SetAPIVersion(apisGroup + "/v1alpha2")
	exports.SetKind("APIExportList")
	require.NoError(t, root.List(t.Context(), exports), "listing APIExports in root")

	ids := identities{}
	for _, export := range exports.Items {
		hash, _, _ := unstructured.NestedString(export.Object, "status", "identityHash")
		resources, _, _ := unstructured.NestedSlice(export.Object, "spec", "resources")
		for _, r := range resources {
			if r, ok := r.(map[string]any); ok {
				ids.add(fmt.Sprint(r["group"]), fmt.Sprint(r["name"]), hash)
			}
		}
	}
	return ids
}

// boundIdentities reads the identity hashes of the resources bound in cl's
// workspace.
func boundIdentities(t testing.TB, cl ctrlruntimeclient.Client) identities {
	t.Helper()
	bindings := &unstructured.UnstructuredList{}
	bindings.SetAPIVersion(apisGroup + "/v1alpha2")
	bindings.SetKind("APIBindingList")
	require.NoError(t, cl.List(t.Context(), bindings), "listing APIBindings")

	ids := identities{}
	for _, binding := range bindings.Items {
		bound, _, _ := unstructured.NestedSlice(binding.Object, "status", "boundResources")
		for _, r := range bound {
			r, ok := r.(map[string]any)
			if !ok {
				continue
			}
			hash, _, _ := unstructured.NestedString(r, "schema", "identityHash")
			ids.add(fmt.Sprint(r["group"]), fmt.Sprint(r["resource"]), hash)
		}
	}
	return ids
}
