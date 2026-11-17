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
