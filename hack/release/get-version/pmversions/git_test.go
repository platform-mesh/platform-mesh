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

package pmversions

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh/hack/release/get-version/scratchrepo"
)

// Tests chdir into a scratch repository, so they cannot run in parallel.

// thisMonth is the development base of a commit made now, vYY.MM.9999.
func thisMonth() string {
	now := time.Now()
	return fmt.Sprintf("v%d.%02d.%d", now.Year()%100, int(now.Month()), developmentPatch)
}

func TestFromCommit(t *testing.T) {
	repo := scratchrepo.NewT(t)
	repo.Commit("initial")
	repo.Tag("v26.10.0")
	release := repo.Head()
	repo.Commit("commit")
	repo.Tag("v26.11.0-rc.1")
	candidate := repo.Head()
	repo.Commit("commit")
	between := repo.Head()
	repo.Commit("commit")
	repo.Commit("commit")

	cases := map[string]struct {
		commit string
		want   string
	}{
		"tagged release":   {release, "v26.10.0"},
		"tagged candidate": {candidate, "v26.11.0-rc.1"},
		"abbreviated hash": {release[:7], "v26.10.0"},
		"untagged on main": {between, thisMonth() + "-dev.3.g" + between[:7]},
		"HEAD":             {"HEAD", thisMonth() + "-dev.5.g" + repo.Head()[:7]},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			v, err := FromCommit(t.Context(), cas.commit)
			require.NoError(t, err)
			assert.Equal(t, cas.want, v.String())
		})
	}

	t.Run("unknown commit", func(t *testing.T) {
		_, err := FromCommit(t.Context(), "0000000")
		require.Error(t, err)
	})
}

func TestPrevious(t *testing.T) {
	repo := scratchrepo.NewT(t)
	repo.Commit("initial")

	_, err := Previous(t.Context())
	require.Error(t, err, "no release tag reachable")

	repo.Tag("v26.10.0")
	repo.Commit("commit")
	repo.Tag("v26.10.1-rc.1")
	repo.Commit("commit")
	repo.Tag("apis/v0.2.0")

	prev, err := Previous(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "v26.10.0", prev.String(), "candidates and module tags are not releases")
}

func TestNext(t *testing.T) {
	repo := scratchrepo.NewT(t)
	repo.Commit("initial")
	repo.Tag("v26.12.3")
	repo.Commit("commit")

	next, err := Next(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "v27.01", next.Minor())
}

func TestNow(t *testing.T) {
	repo := scratchrepo.NewT(t)
	repo.Commit("initial")
	repo.Tag("v26.10.0")

	t.Run("final at HEAD", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.10.0", now.String())
	})

	repo.Commit("commit")
	repo.Tag("v26.11.0-rc.1")

	t.Run("candidate at HEAD", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.11.0-rc.1", now.String())
	})

	repo.Tag("v26.11.0")

	t.Run("final wins over candidate at HEAD", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.11.0", now.String())
	})

	repo.Commit("commit")
	repo.Commit("commit")

	t.Run("main builds a development version of its month", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, thisMonth()+"-dev.4.g"+repo.Head()[:7], now.String(), "counted over the whole history")
	})

	t.Run("run number from environment", func(t *testing.T) {
		t.Setenv(envRunNumber, "42")
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, thisMonth()+"-dev.42.g"+repo.Head()[:7], now.String())
	})

	t.Run("bad run number", func(t *testing.T) {
		t.Setenv(envRunNumber, "forty-two")
		_, err := Now(t.Context())
		require.Error(t, err)
	})

	t.Run("branch from environment", func(t *testing.T) {
		t.Setenv(envRefName, "release-26.11")
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.11.1-dev.2.g"+repo.Head()[:7], now.String())
	})

	repo.Git("checkout", "-q", "-b", "release-26.11")

	t.Run("release branch builds towards the next patch", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.11.1-dev.2.g"+repo.Head()[:7], now.String())
	})

	repo.Git("checkout", "-q", "-b", "release-26.12")

	t.Run("release branch before its first release", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.12.0-dev.2.g"+repo.Head()[:7], now.String())
	})

	repo.Git("checkout", "-q", "--detach")

	t.Run("detached without environment is main", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, thisMonth()+"-dev.4.g"+repo.Head()[:7], now.String())
	})
}

func TestFromCommit_withoutTags(t *testing.T) {
	repo := scratchrepo.NewT(t)
	repo.Commit("one")
	repo.Commit("two")

	v, err := FromCommit(t.Context(), "HEAD")
	require.NoError(t, err)
	assert.Equal(t, thisMonth()+"-dev.2.g"+repo.Head()[:7], v.String(), "a checkout without release tags still gets a version")

	repo.Git("checkout", "-q", "-b", "release-26.11")
	_, err = FromCommit(t.Context(), "HEAD")
	require.Error(t, err, "a release branch needs its release tags")
}

func TestParseDescribe(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		out       string
		wantPrev  string
		wantCount int
		wantErr   bool
	}{
		"at the release":        {"v26.10.0-0-g1a2b3c4", "v26.10.0", 0, false},
		"commits after release": {"v26.10.10-17-g1a2b3c4", "v26.10.10", 17, false},
		"garbage":               {"heads/main", "", 0, true},
		"not a release tag":     {"v1-3-g1a2b3c4", "", 0, true},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			prev, count, err := parseDescribe(cas.out)
			if cas.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, cas.wantPrev, prev.String())
			assert.Equal(t, cas.wantCount, count)
		})
	}
}
