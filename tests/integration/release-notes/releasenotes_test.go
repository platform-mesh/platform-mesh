//go:build integration

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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh/hack/release/release-notes/releasenotes"
	"go.platform-mesh.io/platform-mesh/tests/integration/release-notes/scratchrepo"
)

func scratchRepo(t *testing.T) {
	t.Helper()
	repo := scratchrepo.NewT(t)
	repo.Commit("one")
	repo.Tag("v26.10.0")
	repo.Commit("two")
	repo.Tag("v26.10.1-rc.1")
	repo.Commit("three")
	repo.Tag("v26.10.1")
	repo.Commit("four")
}

func TestResolveRef(t *testing.T) {
	scratchRepo(t)

	tag, err := releasenotes.ResolveRef(t.Context(), "v26.10.1")
	require.NoError(t, err)
	assert.Equal(t, "v26.10.1", tag.Version.String())

	_, err = releasenotes.ResolveRef(t.Context(), "26.10.1")
	require.Error(t, err, "a version without the v prefix is not a ref")

	commit, err := releasenotes.ResolveRef(t.Context(), tag.Commit[:7])
	require.NoError(t, err)
	assert.Equal(t, tag, commit, "an abbreviated hash resolves to the same ref")

	head, err := releasenotes.ResolveRef(t.Context(), "HEAD")
	require.NoError(t, err)
	assert.NotEqual(t, tag.Commit, head.Commit)
	assert.Contains(t, head.Version.String(), ".9999-dev.4.g"+head.Commit[:7], "an untagged commit gets a development version")

	_, err = releasenotes.ResolveRef(t.Context(), "v99.99.99")
	require.Error(t, err)
	_, err = releasenotes.ResolveRef(t.Context(), "")
	require.Error(t, err)
}

func TestDefaultStart(t *testing.T) {
	scratchRepo(t)

	cases := map[string]struct {
		end  string
		want string
	}{
		"before a tagged end":  {"v26.10.1", "v26.10.0"},
		"candidate is skipped": {"v26.10.1-rc.1", "v26.10.0"},
		"untagged end":         {"HEAD", "v26.10.1"},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			end, err := releasenotes.ResolveRef(t.Context(), cas.end)
			require.NoError(t, err)
			start, err := releasenotes.DefaultStart(t.Context(), end)
			require.NoError(t, err)
			assert.Equal(t, cas.want, start.Version.String())
		})
	}
}

// TestCheckRange uses its own history with a side branch. Not parallel: scratchrepo.
func TestCheckRange(t *testing.T) {
	repo := scratchrepo.NewT(t)
	repo.Commit("one")
	repo.Tag("v26.10.0")
	repo.Commit("two")
	repo.Tag("v26.10.1")
	repo.Git("checkout", "-q", "-b", "side", "v26.10.0")
	repo.Commit("side")
	repo.Git("checkout", "-q", "main")

	resolve := func(input string) releasenotes.Ref {
		t.Helper()
		r, err := releasenotes.ResolveRef(t.Context(), input)
		require.NoError(t, err)
		return r
	}
	v0, v1, side := resolve("v26.10.0"), resolve("v26.10.1"), resolve("side")

	require.NoError(t, releasenotes.CheckRange(t.Context(), v0, v1))
	require.Error(t, releasenotes.CheckRange(t.Context(), v1, v0), "inverted range")
	require.Error(t, releasenotes.CheckRange(t.Context(), v1, v1), "same commit")
	require.Error(t, releasenotes.CheckRange(t.Context(), side, v1), "unrelated branch")
}

func TestDefaultStart_noRelease(t *testing.T) {
	scratchRepo(t)

	end, err := releasenotes.ResolveRef(t.Context(), "v26.10.0")
	require.NoError(t, err)
	_, err = releasenotes.DefaultStart(t.Context(), end)
	require.Error(t, err, "nothing before the first release")
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
	write("go.sum", "x\n")
	repo.Commit("tidy")

	const repoURL = "https://github.com/platform-mesh/platform-mesh"
	changes, err := releasenotes.FirstParentLog(t.Context(), repoURL, "v26.10.0", "HEAD")
	require.NoError(t, err)
	require.Len(t, changes, 4, "first-parent history only")
	assert.Equal(t, releasenotes.Change{SHA: changes[0].SHA, Title: "tidy", Repo: repoURL}, changes[0])
	assert.Equal(t, releasenotes.Change{SHA: changes[1].SHA, Title: "Add tool", Repo: repoURL}, changes[1])
	assert.Equal(t, releasenotes.Change{SHA: changes[2].SHA, Title: "Add iam service and portal chart", PR: 11, Repo: repoURL}, changes[2])
	assert.Equal(t, releasenotes.Change{SHA: changes[3].SHA, Title: "Add account operator", PR: 10, Repo: repoURL}, changes[3])

	comps, err := releasenotes.Components(t.Context(), "HEAD")
	require.NoError(t, err)
	require.Len(t, comps, 3)

	grouped, err := releasenotes.GroupChanges(t.Context(), comps, changes)
	require.NoError(t, err)
	byName := map[string][]string{}
	for _, g := range grouped {
		for _, c := range g.Changes {
			byName[g.Component] = append(byName[g.Component], c.Title)
		}
	}
	assert.Equal(t, map[string][]string{
		"account-operator":               {"Add account operator"},
		"iam-service":                    {"Add iam service and portal chart"},
		"portal":                         {"Add iam service and portal chart"},
		releasenotes.RepositoryComponent: {"Add tool"},
	}, byName, "a merge is attributed by all files of its branch, once per component; tidy is noise")
	assert.Equal(t, "account-operator", grouped[0].Component, "sorted by component")
}

// TestVersionsAtCommits reads charts and third-party pins at two commits.
// Not parallel: scratchrepo.
func TestVersionsAtCommits(t *testing.T) {
	repo := scratchrepo.NewT(t)
	write := func(path, content string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		repo.Git("add", path)
	}

	write("charts/portal/Chart.yaml", "appVersion: \"v0.27.20\"\nsources:\n  - https://github.com/platform-mesh/portal\n")
	write("charts/account-operator/Chart.yaml", "appVersion: 0.0.0\n")
	write("charts/common/Chart.yaml", "name: common\n")
	write("charts/README.md", "x\n")
	repo.Commit("start")
	start := repo.Head()

	write("charts/portal/Chart.yaml", "appVersion: \"v0.27.32\"\nsources:\n  - https://example.com/docs\n  - https://github.com/platform-mesh/portal.git\n")
	write("charts/iam-ui/Chart.yaml", "appVersion: \"v0.12.30\"\n")
	write("third_party/kcp/component-constructor.yaml", "components:\n  - name: github.com/kcp-dev/kcp\n    resources:\n      - name: image\n        version: v0.33.6\n      - name: chart\n        version: 0.1.0\n")
	write("third_party/README.md", "x\n")
	repo.Commit("end")
	end := repo.Head()

	t.Run("showFile", func(t *testing.T) {
		data, ok, err := releasenotes.ShowFile(t.Context(), end, "charts/iam-ui/Chart.yaml")
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "appVersion: \"v0.12.30\"\n", string(data))

		_, ok, err = releasenotes.ShowFile(t.Context(), start, "charts/iam-ui/Chart.yaml")
		require.NoError(t, err)
		assert.False(t, ok, "missing file is not an error")

		_, ok, err = releasenotes.ShowFile(t.Context(), end, "charts/portal")
		require.NoError(t, err)
		assert.False(t, ok, "a directory is not a file")

		_, _, err = releasenotes.ShowFile(t.Context(), "deadbeef", "charts/portal/Chart.yaml")
		require.Error(t, err, "an unknown commit is an error, not a missing file")
	})

	t.Run("chartNames", func(t *testing.T) {
		names, err := releasenotes.ChartNames(t.Context(), end)
		require.NoError(t, err)
		assert.Equal(t, []string{"account-operator", "common", "iam-ui", "portal"}, names)
	})

	t.Run("readChart", func(t *testing.T) {
		meta, ok, err := releasenotes.ReadChart(t.Context(), start, "portal")
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "v0.27.20", meta.AppVersion)
		_, ok, err = releasenotes.ReadChart(t.Context(), start, "iam-ui")
		require.NoError(t, err)
		assert.False(t, ok, "chart did not exist yet")
		meta, ok, err = releasenotes.ReadChart(t.Context(), end, "common")
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Empty(t, meta.AppVersion, "no appVersion field")
	})

	t.Run("otherComponentRows", func(t *testing.T) {
		rows, err := releasenotes.OtherComponentRows(t.Context(), start, end)
		require.NoError(t, err)
		want := []releasenotes.VersionRow{
			{Name: "iam-ui", From: releasenotes.Absent, To: "v0.12.30", Changed: true},
			{Name: "portal", From: "v0.27.20", To: "v0.27.32", Changed: true, Source: "https://github.com/platform-mesh/portal"},
		}
		assert.Equal(t, want, rows, "0.0.0 and charts without appVersion are not other components; the first GitHub source wins, .git stripped")
	})

	t.Run("thirdPartyVersions", func(t *testing.T) {
		before, err := releasenotes.ThirdPartyVersions(t.Context(), start)
		require.NoError(t, err)
		assert.Empty(t, before, "no third_party/ yet")
		after, err := releasenotes.ThirdPartyVersions(t.Context(), end)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"kcp/image": "v0.33.6", "kcp/chart": "0.1.0"}, after)
	})
}
