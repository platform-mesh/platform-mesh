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

func TestThirdPartyRows(t *testing.T) {
	t.Parallel()

	from := map[string]string{
		"kcp/image":        "v0.33.4",
		"traefik/chart":    "42.0.0",
		"openfga/image":    "v1.22.1",
		"cert-manager/old": "v1",
	}
	to := map[string]string{
		"kcp/image":           "v0.33.6",
		"traefik/chart":       "42.0.0",
		"openfga/image":       "v1.22.1",
		"api-syncagent/chart": "0.6.1",
	}
	want := []versionRow{
		{Name: "api-syncagent/chart", From: absent, To: "0.6.1", Changed: true},
		{Name: "cert-manager/old", From: "v1", To: removed, Changed: true},
		{Name: "kcp/image", From: "v0.33.4", To: "v0.33.6", Changed: true},
	}
	assert.Equal(t, want, thirdPartyRows(from, to), "sorted, unchanged pins dropped")
	assert.Nil(t, thirdPartyRows(from, from))
}

func TestShippedComponents(t *testing.T) {
	t.Parallel()

	comps := []component{
		{Name: "account-operator", Path: "operators/account-operator", Image: "account-operator"},
		{Name: "apis", Path: "apis", Library: true},
		{Name: "provider-operator", Path: "operators/provider-operator"},
		{Name: "root", Path: "."},
	}
	assert.Equal(t, []string{"account-operator"}, shippedComponents(comps), "libraries and components without an image are not shipped")
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

	write("charts/portal/Chart.yaml", "appVersion: \"v0.27.20\"\n")
	write("charts/account-operator/Chart.yaml", "appVersion: 0.0.0\n")
	write("charts/common/Chart.yaml", "name: common\n")
	write("charts/README.md", "x\n")
	repo.Commit("start")
	start := repo.Head()

	write("charts/portal/Chart.yaml", "appVersion: \"v0.27.32\"\n")
	write("charts/iam-ui/Chart.yaml", "appVersion: \"v0.12.30\"\n")
	write("third_party/kcp/component-constructor.yaml", "components:\n  - name: github.com/kcp-dev/kcp\n    resources:\n      - name: image\n        version: v0.33.6\n      - name: chart\n        version: 0.1.0\n")
	write("third_party/README.md", "x\n")
	repo.Commit("end")
	end := repo.Head()

	t.Run("chartNames", func(t *testing.T) {
		names, err := chartNames(t.Context(), end)
		require.NoError(t, err)
		assert.Equal(t, []string{"account-operator", "common", "iam-ui", "portal"}, names)
	})

	t.Run("appVersion", func(t *testing.T) {
		v, err := appVersion(t.Context(), start, "portal")
		require.NoError(t, err)
		assert.Equal(t, "v0.27.20", v)
		v, err = appVersion(t.Context(), start, "iam-ui")
		require.NoError(t, err)
		assert.Empty(t, v, "chart did not exist yet")
		v, err = appVersion(t.Context(), end, "common")
		require.NoError(t, err)
		assert.Empty(t, v, "no appVersion field")
	})

	t.Run("otherComponentRows", func(t *testing.T) {
		rows, err := otherComponentRows(t.Context(), start, end)
		require.NoError(t, err)
		want := []versionRow{
			{Name: "iam-ui", From: absent, To: "v0.12.30", Changed: true},
			{Name: "portal", From: "v0.27.20", To: "v0.27.32", Changed: true},
		}
		assert.Equal(t, want, rows, "0.0.0 and charts without appVersion are not other components")
	})

	t.Run("thirdPartyVersions", func(t *testing.T) {
		before, err := thirdPartyVersions(t.Context(), start)
		require.NoError(t, err)
		assert.Empty(t, before, "no third_party/ yet")
		after, err := thirdPartyVersions(t.Context(), end)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"kcp/image": "v0.33.6", "kcp/chart": "0.1.0"}, after)
	})
}
