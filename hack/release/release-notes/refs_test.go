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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh/hack/release/get-version/pmversions"
	"go.platform-mesh.io/platform-mesh/hack/release/get-version/scratchrepo"
)

func TestRef_ocmVersion(t *testing.T) {
	t.Parallel()

	v, err := pmversions.ParseTag("v26.11.0-rc.1")
	require.NoError(t, err)
	assert.Equal(t, "26.11.0-rc.1", ref{version: v}.ocmVersion())
}

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

	tag, err := resolveRef(t.Context(), "v26.10.1")
	require.NoError(t, err)
	assert.Equal(t, "v26.10.1", tag.version.String())

	_, err = resolveRef(t.Context(), "26.10.1")
	require.Error(t, err, "a version without the v prefix is not a ref")

	commit, err := resolveRef(t.Context(), tag.commit[:7])
	require.NoError(t, err)
	assert.Equal(t, tag, commit, "an abbreviated hash resolves to the same ref")

	head, err := resolveRef(t.Context(), "HEAD")
	require.NoError(t, err)
	assert.NotEqual(t, tag.commit, head.commit)
	assert.Equal(t, "v26.11.0-dev.1.g"+head.commit[:7], head.version.String(), "an untagged commit gets a prerelease version")

	_, err = resolveRef(t.Context(), "v99.99.99")
	require.Error(t, err)
	_, err = resolveRef(t.Context(), "")
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
			end, err := resolveRef(t.Context(), cas.end)
			require.NoError(t, err)
			start, err := defaultStart(t.Context(), end)
			require.NoError(t, err)
			assert.Equal(t, cas.want, start.version.String())
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

	resolve := func(input string) ref {
		t.Helper()
		r, err := resolveRef(t.Context(), input)
		require.NoError(t, err)
		return r
	}
	v0, v1, side := resolve("v26.10.0"), resolve("v26.10.1"), resolve("side")

	require.NoError(t, checkRange(t.Context(), v0, v1))
	require.Error(t, checkRange(t.Context(), v1, v0), "inverted range")
	require.Error(t, checkRange(t.Context(), v1, v1), "same commit")
	require.Error(t, checkRange(t.Context(), side, v1), "unrelated branch")
}

func TestDefaultStart_noRelease(t *testing.T) {
	scratchRepo(t)

	end, err := resolveRef(t.Context(), "v26.10.0")
	require.NoError(t, err)
	_, err = defaultStart(t.Context(), end)
	require.Error(t, err, "nothing before the first release")
}
