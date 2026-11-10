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
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests chdir into a scratch repository and set GITHUB_* variables, so they
// cannot run in parallel.

// scratchRepo creates a git repository on branch main with one commit and
// makes it the working directory.
func scratchRepo(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())

	// Use the default git config so developers' config doesn't
	// interfere
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	t.Setenv(envRefName, "")
	t.Setenv(envRunNumber, "")
	run(t, "init", "-q", "-b", "main")
	run(t, "config", "user.name", "test")
	run(t, "config", "user.email", "test@example.com")
	commit(t)
}

func run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T) {
	t.Helper()
	run(t, "commit", "-q", "--allow-empty", "-m", "commit")
}

func sha(t *testing.T) string {
	t.Helper()
	return run(t, "rev-parse", "--short=7", "HEAD")
}

func TestFromCommit(t *testing.T) {
	scratchRepo(t)
	run(t, "tag", "v26.10.0")
	release := run(t, "rev-parse", "HEAD")
	commit(t)
	run(t, "tag", "v26.11.0-rc.1")
	candidate := run(t, "rev-parse", "HEAD")
	commit(t)
	between := run(t, "rev-parse", "HEAD")
	commit(t)
	commit(t)

	cases := map[string]struct {
		commit string
		want   string
	}{
		"tagged release":               {release, "v26.10.0"},
		"tagged candidate":             {candidate, "v26.11.0-rc.1"},
		"abbreviated hash":             {release[:7], "v26.10.0"},
		"untagged counts from release": {between, "v26.11.0-dev.2.g" + between[:7]},
		"HEAD":                         {"HEAD", "v26.11.0-dev.4.g" + sha(t)},
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
	scratchRepo(t)

	_, err := Previous(t.Context())
	require.Error(t, err, "no release tag reachable")

	run(t, "tag", "v26.10.0")
	commit(t)
	run(t, "tag", "v26.10.1-rc.1")
	commit(t)
	run(t, "tag", "apis/v0.2.0")

	prev, err := Previous(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "v26.10.0", prev.String(), "candidates and module tags are not releases")
}

func TestNext(t *testing.T) {
	scratchRepo(t)
	run(t, "tag", "v26.12.3")
	commit(t)

	next, err := Next(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "v27.01", next.Minor())
}

func TestNow(t *testing.T) {
	scratchRepo(t)
	run(t, "tag", "v26.10.0")

	t.Run("final at HEAD", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.10.0", now.String())
	})

	commit(t)
	run(t, "tag", "v26.11.0-rc.1")

	t.Run("candidate at HEAD", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.11.0-rc.1", now.String())
	})

	run(t, "tag", "v26.11.0")

	t.Run("final wins over candidate at HEAD", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.11.0", now.String())
	})

	commit(t)
	commit(t)

	t.Run("main builds towards the next feature release", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.12.0-dev.2.g"+sha(t), now.String())
	})

	t.Run("run number from environment", func(t *testing.T) {
		t.Setenv(envRunNumber, "42")
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.12.0-dev.42.g"+sha(t), now.String())
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
		assert.Equal(t, "v26.11.1-dev.2.g"+sha(t), now.String())
	})

	run(t, "checkout", "-q", "-b", "release-26.11")

	t.Run("release branch builds towards the next patch", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.11.1-dev.2.g"+sha(t), now.String())
	})

	run(t, "checkout", "-q", "-b", "release-26.12")

	t.Run("release branch before its first release", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.12.0-dev.2.g"+sha(t), now.String())
	})

	run(t, "checkout", "-q", "--detach")

	t.Run("detached without environment is main", func(t *testing.T) {
		now, err := Now(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "v26.12.0-dev.2.g"+sha(t), now.String())
	})
}

func TestParseDescribe(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		out       string
		wantPrev  string
		wantCount int
		wantSHA   string
		wantErr   bool
	}{
		"at the release":        {"v26.10.0-0-g1a2b3c4", "v26.10.0", 0, "1a2b3c4", false},
		"commits after release": {"v26.10.10-17-g1a2b3c4", "v26.10.10", 17, "1a2b3c4", false},
		"garbage":               {"heads/main", "", 0, "", true},
		"not a release tag":     {"v1-3-g1a2b3c4", "", 0, "", true},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			prev, count, sha, err := parseDescribe(cas.out)
			if cas.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, cas.wantPrev, prev.String())
			assert.Equal(t, cas.wantCount, count)
			assert.Equal(t, cas.wantSHA, sha)
		})
	}
}
