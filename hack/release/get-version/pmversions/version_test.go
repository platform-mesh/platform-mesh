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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTag(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		tag     string
		want    Version
		wantErr bool
	}{
		"final":             {"v26.11.0", Version{Year: 26, Month: 11, Patch: 0}, false},
		"patch":             {"v26.10.12", Version{Year: 26, Month: 10, Patch: 12}, false},
		"release candidate": {"v26.11.0-rc.3", Version{Year: 26, Month: 11, Patch: 0, Pre: "rc.3"}, false},
		"dev build":         {"v26.11.0-dev.42.g1a2b3c4", Version{Year: 26, Month: 11, Patch: 0, Pre: "dev.42.g1a2b3c4"}, false},
		"missing v":         {"26.11.0", Version{}, true},
		"two components":    {"v26.11", Version{}, true},
		"module tag":        {"apis/v0.2.0", Version{}, true},
		"month zero":        {"v26.00.0", Version{}, true},
		"month thirteen":    {"v26.13.0", Version{}, true},
		"empty suffix":      {"v26.11.0-", Version{}, true},
		"garbage":           {"latest", Version{}, true},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			got, err := ParseTag(cas.tag)
			if cas.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, cas.want, got)
			assert.Equal(t, cas.tag, got.String(), "String must round-trip the tag")
		})
	}
}

func TestParseReleaseBranch(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		branch string
		want   Version
		wantOK bool
	}{
		"release branch": {"release-26.11", Version{Year: 26, Month: 11}, true},
		"main":           {"main", Version{}, false},
		"detached":       {"HEAD", Version{}, false},
		"with patch":     {"release-26.11.1", Version{}, false},
		"bad month":      {"release-26.13", Version{}, false},
		"feature branch": {"fix-release-26.11", Version{}, false},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseReleaseBranch(cas.branch)
			assert.Equal(t, cas.wantOK, ok)
			assert.Equal(t, cas.want, got)
		})
	}
}

func TestVersion_LibraryString(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		tag  string
		want string
	}{
		"final":             {"v26.11.0", "v0.2611.0"},
		"patch":             {"v26.10.12", "v0.2610.12"},
		"january is padded": {"v27.01.0", "v0.2701.0"},
		"release candidate": {"v26.11.0-rc.3", "v0.2611.0-rc.3"},
		"dev build":         {"v26.11.0-dev.42.g1a2b3c4", "v0.2611.0-dev.42.g1a2b3c4"},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			v, err := ParseTag(cas.tag)
			require.NoError(t, err)
			assert.Equal(t, cas.want, v.LibraryString())
		})
	}
}

func TestVersion_Compare(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		a, b string
		want int
	}{
		"year":                      {"v25.12.9", "v26.01.0", -1},
		"month":                     {"v26.10.9", "v26.11.0", -1},
		"patch numeric not lexical": {"v26.10.9", "v26.10.10", -1},
		"rc below final":            {"v26.11.0-rc.1", "v26.11.0", -1},
		"final above rc":            {"v26.11.0", "v26.11.0-rc.1", 1},
		"rc order":                  {"v26.11.0-rc.1", "v26.11.0-rc.2", -1},
		"equal":                     {"v26.11.0", "v26.11.0", 0},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			a, err := ParseTag(cas.a)
			require.NoError(t, err)
			b, err := ParseTag(cas.b)
			require.NoError(t, err)
			assert.Equal(t, cas.want, a.Compare(b))
		})
	}
}

func TestVersion_NextMinor(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		version Version
		want    Version
	}{
		"within year":  {Version{Year: 26, Month: 11}, Version{Year: 26, Month: 12}},
		"year wrap":    {Version{Year: 26, Month: 12}, Version{Year: 27, Month: 1}},
		"drops patch":  {Version{Year: 26, Month: 11, Patch: 3}, Version{Year: 26, Month: 12}},
		"drops suffix": {Version{Year: 26, Month: 11, Pre: "rc.1"}, Version{Year: 26, Month: 12}},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, cas.want, cas.version.NextMinor())
		})
	}
}

func TestVersion_Minor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "v27.01", Version{Year: 27, Month: 1, Patch: 2}.Minor(), "month is zero-padded, patch dropped")
	assert.Equal(t, "v26.11", Version{Year: 26, Month: 11}.Minor())
}

func TestVersion_SameMinor(t *testing.T) {
	t.Parallel()

	a := Version{Year: 26, Month: 11, Patch: 2, Pre: "rc.1"}
	assert.True(t, a.SameMinor(Version{Year: 26, Month: 11}), "patch and suffix do not matter")
	assert.False(t, a.SameMinor(Version{Year: 26, Month: 12}))
	assert.False(t, a.SameMinor(Version{Year: 27, Month: 11}))
}
