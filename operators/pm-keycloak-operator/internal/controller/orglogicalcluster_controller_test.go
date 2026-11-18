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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/pm-keycloak-operator/pkg/config"
)

func TestOrgSubroutines(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		idp, invite, auth bool
		expected          []string
	}{
		"defaults":           {true, true, true, []string{"IDPSubroutine", "InviteInitializationSubroutine", "workspaceAuthConfiguration"}},
		"idp off":            {false, true, true, []string{"InviteInitializationSubroutine", "workspaceAuthConfiguration"}},
		"invite off":         {true, false, true, []string{"IDPSubroutine", "workspaceAuthConfiguration"}},
		"workspace auth off": {true, true, false, []string{"IDPSubroutine", "InviteInitializationSubroutine"}},
		"all off":            {false, false, false, []string{}},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			cfg := config.NewConfig()
			cfg.Initializer.IDPEnabled = cas.idp
			cfg.Initializer.InviteEnabled = cas.invite
			cfg.Initializer.WorkspaceAuthEnabled = cas.auth

			subs, err := orgSubroutines(cfg, nil, nil, nil)
			require.NoError(t, err)

			names := make([]string, 0, len(subs))
			for _, sub := range subs {
				names = append(names, sub.GetName())
			}
			assert.Equal(t, cas.expected, names)
		})
	}
}
