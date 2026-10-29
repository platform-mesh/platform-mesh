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

package composition

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gobuffalo/flect"
	"github.com/stretchr/testify/require"
)

// The portal queries the generated type as a GraphQL field the gateway names
// flect.Pluralize(Kind), NOT the lowercase resource plural: WebPage yields WebPages, not
// webpages.
func TestBuildContentConfig_EntityCollectionMatchesFlect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ kind, plural string }{
		{"WebPage", "webpages"},
		{"Policy", "policies"},
		{"Bundle", "bundles"},
	} {
		cc := BuildContentConfig("apps.example.com", "v1alpha1", tc.kind, tc.plural, true, nil)
		require.Contains(t, cc, `"entityCollection":"`+flect.Pluralize(tc.kind)+`"`, "kind %q", tc.kind)
		if flect.Pluralize(tc.kind) != tc.plural {
			require.NotContains(t, cc, `"entityCollection":"`+tc.plural+`"`, "kind %q must not use the resource plural", tc.kind)
		}
	}
}

func TestBuildContentConfig_ScopeAndFields(t *testing.T) {
	t.Parallel()
	// Namespaced type with a spec field: valid JSON, Namespaced scope, spec field present.
	cc := BuildContentConfig("apps.example.com", "v1alpha1", "WebPage", "webpages", true, []string{"url"})
	require.True(t, json.Valid([]byte(cc)), "content config must be valid JSON")
	for _, want := range []string{`"scope":"Namespaced"`, `"spec.url"`, "WebPage (kro)", "metadata.namespace"} {
		require.Contains(t, cc, want)
	}

	// Namespace belongs to the list/detail columns only: the portal renders its own
	// namespace selector, so a createView field would duplicate that control and pin it
	// to a fixed list.
	var parsed struct {
		LuigiConfigFragment struct {
			Data struct {
				Nodes []struct {
					Context struct {
						ResourceDefinition struct {
							UI struct {
								CreateView struct {
									Fields []map[string]any `json:"fields"`
								} `json:"createView"`
								ListView struct {
									Fields []map[string]any `json:"fields"`
								} `json:"listView"`
							} `json:"ui"`
						} `json:"resourceDefinition"`
					} `json:"context"`
				} `json:"nodes"`
			} `json:"data"`
		} `json:"luigiConfigFragment"`
	}
	require.NoError(t, json.Unmarshal([]byte(cc), &parsed))
	ui := parsed.LuigiConfigFragment.Data.Nodes[0].Context.ResourceDefinition.UI
	properties := func(fields []map[string]any) []string {
		out := make([]string, 0, len(fields))
		for _, f := range fields {
			out = append(out, f["property"].(string))
		}
		return out
	}
	require.NotContains(t, properties(ui.CreateView.Fields), "metadata.namespace",
		"createView must not declare a namespace field; the portal owns that control")
	require.Contains(t, properties(ui.ListView.Fields), "metadata.namespace",
		"listView should still show the namespace column")

	require.Equal(t, 2, strings.Count(cc, `"namespace":{"read":true,"write":true}`),
		"namespaced list and detail nodes must grant namespace access")

	// Cluster-scoped type: Cluster scope and no namespace field.
	ccCluster := BuildContentConfig("apps.example.com", "v1alpha1", "WebPage", "webpages", false, nil)
	require.Contains(t, ccCluster, `"scope":"Cluster"`)
	require.NotContains(t, ccCluster, "metadata.namespace", "cluster-scoped type must not reference a namespace")
	require.Equal(t, 1, strings.Count(ccCluster, `"namespace":{"read":true,"write":true}`),
		"only the list needs permission to clear a stale namespace")
}

func TestTitle(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"": "", "url": "Url", "Already": "Already"} {
		require.Equal(t, want, title(in), "title(%q)", in)
	}
}

func TestValidateAPIGroup(t *testing.T) {
	t.Parallel()

	t.Run("accepted", func(t *testing.T) {
		t.Parallel()
		for _, group := range []string{
			"vault.demo.io", // the demo groups that work
			"fleet.demo.io",
			"apps.example.com",
			"kro.run",
			"webpage.demo.io",
			"single",         // no dots
			"with9digits.io", // digits are fine as long as a segment does not start with one
		} {
			require.NoError(t, ValidateAPIGroup(group), "group %q should be accepted", group)
		}
	})

	t.Run("rejected", func(t *testing.T) {
		t.Parallel()
		for _, group := range []string{
			"demo.platform-mesh.dev", // the group that actually broke the portal
			"my-app.demo.io",         // hyphen in the first segment
			"a.b-c.d",
			"9starts.with.digit", // a legal DNS subdomain, but not a legal GraphQL name
			// Not legal API groups either, but the check should not accept them.
			"has space.io",
			"trailing.",
			"",
		} {
			require.Error(t, ValidateAPIGroup(group), "group %q should be rejected", group)
		}
	})

	t.Run("message names the offending segment and suggests a fix", func(t *testing.T) {
		t.Parallel()
		err := ValidateAPIGroup("demo.platform-mesh.dev")
		require.ErrorContains(t, err, "platform-mesh")
		require.ErrorContains(t, err, "vault.demo.io")
	})

	// Anything ValidateAPIGroup accepts must survive BuildContentConfig's dot->underscore
	// mapping as a valid GraphQL identifier. The two have to agree.
	t.Run("accepted groups map to valid GraphQL names", func(t *testing.T) {
		t.Parallel()
		for _, group := range []string{"vault.demo.io", "kro.run", "apps.example.com"} {
			require.NoError(t, ValidateAPIGroup(group))
			require.Regexp(t, `^[_A-Za-z][_0-9A-Za-z]*$`, strings.ReplaceAll(group, ".", "_"))
		}
	})
}
