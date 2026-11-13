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

package generator

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	pmgateway "go.platform-mesh.io/apis/gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/apischema"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/kube-openapi/pkg/schemamutation"
	"k8s.io/kube-openapi/pkg/spec3"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// This is a regression test for an incident where an entire API group silently
// disappeared from the generated GraphQL schema. Root cause: the listener's
// scope enricher failed to resolve scope for the group, so the schemas shipped
// without the x-kubernetes-scope extension, and parseResources then silently
// dropped every resource lacking that extension.
//
// The fixture (testdata/synthetic_crd.json) is a hand-written, fictional CRD
// group (example.com) that mirrors the structural shape of a real CRD OpenAPI
// doc (GVK extension, spec/status, no scope). It deliberately contains no real
// resource definitions.

const (
	syntheticFixture = "testdata/synthetic_crd.json"
	syntheticGroup   = "example.com"
)

// reproRefWalker mirrors listener/pkg/apischema/loader.go createRefWalker():
// it normalizes $ref pointers down to just the final token, matching what the
// gateway actually receives.
func reproRefWalker() schemamutation.Walker {
	return schemamutation.Walker{
		RefCallback: schemamutation.RefCallbackNoop,
		SchemaCallback: func(sch *spec.Schema) *spec.Schema {
			refPtr := sch.Ref.GetPointer()
			if refPtr == nil {
				return sch
			}
			tokens := refPtr.DecodedTokens()
			if len(tokens) == 0 {
				return sch
			}
			resolvedRef := tokens[len(tokens)-1]
			return &spec.Schema{
				SchemaProps: spec.SchemaProps{
					Ref: spec.MustCreateRef(resolvedRef),
				},
			}
		},
	}
}

// loadReproDefs replicates the listener loader: unmarshal a spec3 OpenAPI doc,
// walk/normalize refs per component, key entries by component name.
func loadReproDefs(t *testing.T, path string) map[string]*spec.Schema {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var doc spec3.OpenAPI
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotNil(t, doc.Components)

	walker := reproRefWalker()
	out := make(map[string]*spec.Schema)
	for key, sch := range doc.Components.Schemas {
		out[key] = walker.WalkSchema(sch)
	}
	return out
}

// addScopeExtension simulates the listener's scope enricher succeeding: it adds
// x-kubernetes-scope to every schema carrying a GVK extension, which is what a
// working RESTMapper / discovery lookup produces.
func addScopeExtension(defs map[string]*spec.Schema) {
	for _, sch := range defs {
		gvk, err := apischema.ExtractGVK(sch)
		if err != nil || gvk == nil {
			continue
		}
		scope := apiextensionsv1.NamespaceScoped
		if strings.HasPrefix(gvk.Kind, "Cluster") {
			scope = apiextensionsv1.ClusterScoped
		}
		sch.AddExtension(pmgateway.ScopeExtensionKey, scope)
	}
}

func groupSubscriptionFields(t *testing.T, defs map[string]*spec.Schema, groupPrefix string) []string {
	t.Helper()
	g := New(defs, nil, nil, false)
	gqlSchema, err := g.Generate(context.Background())
	require.NoError(t, err)
	var got []string
	for name := range gqlSchema.SubscriptionType().Fields() {
		if strings.HasPrefix(name, groupPrefix) {
			got = append(got, name)
		}
	}
	sort.Strings(got)
	return got
}

// TestScopeMissingDropsResources documents the failure mode: a resource whose
// schema lacks x-kubernetes-scope is dropped by parseResources.
func TestScopeMissingDropsResources(t *testing.T) {
	defs := loadReproDefs(t, syntheticFixture)

	var parsed int
	for _, r := range (&SchemaGenerator{definitions: defs}).parseResources(context.Background()) {
		if r.GVK.Group == syntheticGroup {
			parsed++
		}
	}
	require.Zero(t, parsed,
		"without x-kubernetes-scope, resources are expected to be dropped (the bug)")
}

// TestScopePresentExposesGroup is the regression guard: once scope is present,
// all resources in the group must appear as subscription fields.
func TestScopePresentExposesGroup(t *testing.T) {
	defs := loadReproDefs(t, syntheticFixture)
	addScopeExtension(defs)

	got := groupSubscriptionFields(t, defs, "example_com_")
	require.ElementsMatch(t, []string{
		"example_com_v1alpha1_clusterwidget",
		"example_com_v1alpha1_clusterwidgets",
		"example_com_v1alpha1_widget",
		"example_com_v1alpha1_widgets",
	}, got, "all resources in the group should be exposed once scope is resolved")
}
