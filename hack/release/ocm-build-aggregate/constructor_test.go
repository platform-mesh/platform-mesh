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

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

const testConstructor = `components:
  - name: github.com/platform-mesh/platform-mesh
    version: ${PM_VERSION}
    componentReferences:
      - name: account-operator
        componentName: github.com/platform-mesh/account-operator
        version: ${PM_VERSION}
      - name: terminal-controller-manager
        componentName: github.com/platform-mesh/terminal-controller-manager
        version: ${PM_VERSION}
      - name: pinned
        componentName: github.com/example/pinned
        # a version that must stay a string
        version: 0.10
`

func TestWithoutReferences(t *testing.T) {
	skipped := map[string]releaseutils.Component{"terminal-controller-manager": {}}

	out, dropped, err := withoutReferences([]byte(testConstructor), skipped)
	require.NoError(t, err)
	require.Equal(t, []string{"terminal-controller-manager"}, dropped)
	require.NotContains(t, string(out), "terminal-controller-manager")
	require.Contains(t, string(out), "github.com/platform-mesh/account-operator")
	require.Contains(t, string(out), "version: ${PM_VERSION}")
	require.Contains(t, string(out), "version: 0.10\n")
}

func TestWithoutReferencesNothingSkipped(t *testing.T) {
	out, dropped, err := withoutReferences([]byte(testConstructor), nil)
	require.NoError(t, err)
	require.Empty(t, dropped)
	require.Contains(t, string(out), "terminal-controller-manager")
}
