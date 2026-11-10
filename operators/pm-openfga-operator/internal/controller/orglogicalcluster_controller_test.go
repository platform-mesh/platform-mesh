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

package controller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/security-operator/pkg/config"
)

func TestOrgSubroutines(t *testing.T) {
	t.Parallel()

	coreModule := filepath.Join(t.TempDir(), "core.fga")
	require.NoError(t, os.WriteFile(coreModule, []byte("module core\n"), 0o600))

	cases := map[string]struct {
		workspace bool
		rekey     bool
		expected  []string
	}{
		"defaults":           {true, false, []string{"WorkspaceInitializer"}},
		"rekey enabled":      {true, true, []string{"WorkspaceInitializer", "RekeyTuplesSubroutine"}},
		"workspace disabled": {false, true, []string{"RekeyTuplesSubroutine"}},
		"all off":            {false, false, []string{}},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			cfg := config.NewConfig()
			cfg.CoreModulePath = coreModule
			cfg.Initializer.WorkspaceInitializerEnabled = cas.workspace
			cfg.RekeyOrphanedTuplesEnabled = cas.rekey

			subs := orgSubroutines(cfg, nil, nil, nil, nil)

			names := make([]string, 0, len(subs))
			for _, sub := range subs {
				names = append(names, sub.GetName())
			}
			assert.Equal(t, cas.expected, names)
		})
	}
}
