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

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh/hack/release/get-version/scratchrepo"
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

	comps := []component{
		{Name: "root", Path: "."},
		{Name: "apis", Path: "apis", Library: true},
		{Name: "account-operator", Path: "operators/account-operator", Chart: "charts/account-operator"},
		{Name: "account-operator-crds", Path: "operators/account-operator/crds"},
	}

	cases := map[string]string{
		"operators/account-operator/main.go":         "account-operator",
		"charts/account-operator/values.yaml":        "account-operator",
		"operators/account-operator/crds/x.yaml":     "account-operator-crds",
		"operators/account-operator-other/main.go":   repositoryComponent,
		"apis/core/v1alpha1/types.go":                "apis",
		"charts/portal/Chart.yaml":                   "portal",
		"charts/README.md":                           repositoryComponent,
		"third_party/kcp/component-constructor.yaml": thirdPartyComponent,
		"hack/release/release-notes/main.go":         repositoryComponent,
		"go.work":                                    repositoryComponent,
	}

	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, componentOf(path, comps))
		})
	}
}

// TestChanges builds a history with a squash commit, a merge commit and a
// direct commit touching different components. Not parallel: scratchrepo.
func TestChanges(t *testing.T) {
	repo := scratchrepo.NewT(t)
	write := func(path, content string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		repo.Git("add", path)
	}

	write("ocm/components.yaml", "components:\n  root:\n    path: .\n  account-operator:\n    path: operators/account-operator\n    chart: charts/account-operator\n  iam-service:\n    path: services/iam-service\n")
	repo.Commit("Initial")
	repo.Tag("v26.10.0")

	write("operators/account-operator/main.go", "package main\n")
	write("charts/account-operator/values.yaml", "a: 1\n")
	repo.Commit("Add account operator (#10)")

	repo.Git("checkout", "-q", "-b", "feature")
	write("services/iam-service/main.go", "package main\n")
	repo.Commit("Add iam service")
	write("charts/portal/Chart.yaml", "appVersion: v1\n")
	repo.Commit("Add portal chart")
	repo.Git("checkout", "-q", "main")
	repo.Git("merge", "-q", "--no-ff", "-m", "Merge pull request #11 from x/feature\n\nAdd iam service and portal chart", "feature")

	write("hack/tool.sh", "#!/bin/sh\n")
	repo.Commit("Add tool")

	changes, err := firstParentLog(t.Context(), "v26.10.0", "HEAD")
	require.NoError(t, err)
	require.Len(t, changes, 3, "first-parent history only")
	assert.Equal(t, change{SHA: changes[0].SHA, Title: "Add tool"}, changes[0])
	assert.Equal(t, change{SHA: changes[1].SHA, Title: "Add iam service and portal chart", PR: 11}, changes[1])
	assert.Equal(t, change{SHA: changes[2].SHA, Title: "Add account operator", PR: 10}, changes[2])

	comps, err := components(t.Context(), "HEAD")
	require.NoError(t, err)
	require.Len(t, comps, 3)

	grouped, err := groupChanges(t.Context(), comps, changes)
	require.NoError(t, err)
	byName := map[string][]string{}
	for _, g := range grouped {
		for _, c := range g.Changes {
			byName[g.Component] = append(byName[g.Component], c.Title)
		}
	}
	assert.Equal(t, map[string][]string{
		"account-operator":  {"Add account operator"},
		"iam-service":       {"Add iam service and portal chart"},
		"portal":            {"Add iam service and portal chart"},
		repositoryComponent: {"Add tool"},
	}, byName, "a merge is attributed by all files of its branch, once per component")
	assert.Equal(t, "account-operator", grouped[0].Component, "sorted by component")
}
