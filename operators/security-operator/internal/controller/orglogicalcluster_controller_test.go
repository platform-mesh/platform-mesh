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
	"go.platform-mesh.io/security-operator/internal/features"
)

func TestOrgSubroutines(t *testing.T) {
	t.Parallel()

	coreModule := filepath.Join(t.TempDir(), "core.fga")
	require.NoError(t, os.WriteFile(coreModule, []byte("module core\n"), 0o600))

	cases := map[string]struct {
		gates    string
		rekey    bool
		expected []string
	}{
		"defaults": {
			gates:    "",
			expected: []string{"WorkspaceInitializer", "IDPSubroutine", "InviteInitializationSubroutine", "workspaceAuthConfiguration"},
		},
		"rekey enabled": {
			gates:    "",
			rekey:    true,
			expected: []string{"WorkspaceInitializer", "IDPSubroutine", "InviteInitializationSubroutine", "workspaceAuthConfiguration", "RekeyTuplesSubroutine"},
		},
		"keycloak gate off drops the identity subroutines": {
			gates:    "Keycloak=false",
			expected: []string{"WorkspaceInitializer"},
		},
		"keycloak gate off keeps rekey": {
			gates:    "Keycloak=false",
			rekey:    true,
			expected: []string{"WorkspaceInitializer", "RekeyTuplesSubroutine"},
		},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			cfg := config.NewConfig()
			cfg.CoreModulePath = coreModule
			cfg.RekeyOrphanedTuplesEnabled = cas.rekey
			if cas.gates != "" {
				require.NoError(t, cfg.FeatureGates.Set(cas.gates))
			}

			subs, err := orgSubroutines(cfg, nil, nil, nil, nil, nil)
			require.NoError(t, err)

			names := make([]string, 0, len(subs))
			for _, sub := range subs {
				names = append(names, sub.GetName())
			}
			assert.Equal(t, cas.expected, names)
		})
	}
}

func TestOrgSubroutines_InitializerTogglesStillApply(t *testing.T) {
	t.Parallel()

	coreModule := filepath.Join(t.TempDir(), "core.fga")
	require.NoError(t, os.WriteFile(coreModule, []byte("module core\n"), 0o600))

	cfg := config.NewConfig()
	cfg.CoreModulePath = coreModule
	cfg.Initializer.IDPEnabled = false
	require.True(t, cfg.FeatureEnabled(features.Keycloak), "gate stays on; only the toggle is off")

	subs, err := orgSubroutines(cfg, nil, nil, nil, nil, nil)
	require.NoError(t, err)

	names := make([]string, 0, len(subs))
	for _, sub := range subs {
		names = append(names, sub.GetName())
	}
	assert.Equal(t, []string{"WorkspaceInitializer", "InviteInitializationSubroutine", "workspaceAuthConfiguration"}, names)
}
