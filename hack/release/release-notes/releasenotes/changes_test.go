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

package releasenotes

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLogEntry(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		subject   string
		body      string
		wantTitle string
		wantPR    int
	}{
		"merge commit":              {"Merge pull request #594 from mjudeikis/delete.migration.operator", "Delete kcp-migration-operator\n\nSigned-off-by: x", "Delete kcp-migration-operator", 594},
		"merge commit without body": {"Merge pull request #594 from x/y", "", "Merge pull request #594 from x/y", 594},
		"squash commit":             {"Drop upstream images (#596)", "", "Drop upstream images", 596},
		"direct commit":             {"Add scratchrepo", "details", "Add scratchrepo", 0},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			gotTitle, gotPR := parseLogEntry(cas.subject, cas.body)
			assert.Equal(t, cas.wantTitle, gotTitle)
			assert.Equal(t, cas.wantPR, gotPR)
		})
	}
}

func TestComponentOf(t *testing.T) {
	t.Parallel()

	comps := []Component{
		{Name: "root", Path: "."},
		{Name: "apis", Path: "apis", Library: true},
		{Name: "account-operator", Path: "operators/account-operator", Chart: "charts/account-operator"},
		{Name: "account-operator-crds", Path: "operators/account-operator/crds"},
	}

	cases := map[string]string{
		"operators/account-operator/main.go":         "account-operator",
		"charts/account-operator/values.yaml":        "account-operator",
		"operators/account-operator/crds/x.yaml":     "account-operator-crds",
		"operators/account-operator-other/main.go":   RepositoryComponent,
		"apis/core/v1alpha1/types.go":                "apis",
		"charts/portal/Chart.yaml":                   "portal",
		"charts/README.md":                           RepositoryComponent,
		"third_party/kcp/component-constructor.yaml": thirdPartyComponent,
		"hack/release/release-notes/main.go":         RepositoryComponent,
		"go.work":                                    RepositoryComponent,
	}

	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, componentOf(path, comps))
		})
	}
}

func TestIsNoise(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"Set appVersion v0.27.32 in charts/portal/Chart.yaml":                            true,
		"[BACKPORT] Set appVersion v0.6.7 in charts/example-httpbin-operator/Chart.yaml": true,
		"tidy":                                true,
		"Tidy":                                true,
		"go mod tidy":                         true,
		"codegen":                             true,
		"generate":                            true,
		"Set appVersion for portal":           false,
		"tidy up the release notes":           false,
		"fix(portal): generate correct links": false,
	}

	for title, want := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isNoise(title))
		})
	}
}

func TestMergeUpstream(t *testing.T) {
	t.Parallel()

	const local = "https://github.com/platform-mesh/platform-mesh"
	grouped := []ComponentChanges{
		{Component: "account-operator", Changes: []Change{{SHA: "a", Title: "Fix", PR: 1, Repo: local}}},
		{Component: "portal", Changes: []Change{{SHA: "b", Title: "Tune values", PR: 2, Repo: local}}},
	}
	other := []upstreamChanges{
		{
			Chart: "portal", Repo: "https://github.com/platform-mesh/portal", From: "v0.27.20", To: "v0.27.32",
			Commits: []upstreamCommit{
				{SHA: "c", Title: "feat: x", PR: 10},
				{SHA: "d", Title: "codegen"},
			},
		},
		{
			Chart: "iam-ui", Repo: "https://github.com/platform-mesh/iam-ui", From: "v0.12.23", To: "v0.12.30",
			Commits: []upstreamCommit{{SHA: "e", Title: "fix: y", PR: 20}},
		},
	}

	want := []ComponentChanges{
		{Component: "account-operator", Changes: []Change{{SHA: "a", Title: "Fix", PR: 1, Repo: local}}},
		{Component: "iam-ui", From: "v0.12.23", To: "v0.12.30", Changes: []Change{
			{SHA: "e", Title: "fix: y", PR: 20, Repo: "https://github.com/platform-mesh/iam-ui"},
		}},
		{Component: "portal", From: "v0.27.20", To: "v0.27.32", Changes: []Change{
			{SHA: "c", Title: "feat: x", PR: 10, Repo: "https://github.com/platform-mesh/portal"},
			{SHA: "b", Title: "Tune values", PR: 2, Repo: local},
		}},
	}
	assert.Equal(t, want, mergeUpstream(grouped, other), "upstream commits first, noise dropped, new group created, sorted")
}
