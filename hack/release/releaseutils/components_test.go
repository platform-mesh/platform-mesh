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

package releaseutils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectOptional(t *testing.T) {
	components := map[string]Component{
		"account-operator":            {Name: "account-operator", Path: "operators/account-operator"},
		"terminal-controller-manager": {Name: "terminal-controller-manager", Path: "operators/terminal-controller-manager", Optional: true},
	}

	skipped, err := selectOptional(components, []string{"terminal-controller-manager"})
	require.NoError(t, err)
	require.Contains(t, skipped, "terminal-controller-manager")
	require.Len(t, skipped, 1)

	_, err = selectOptional(components, []string{"account-operator"})
	require.ErrorContains(t, err, "is not optional")
	require.ErrorContains(t, err, "terminal-controller-manager")

	_, err = selectOptional(components, []string{"nope"})
	require.ErrorContains(t, err, "is not a component")
}

func TestParseList(t *testing.T) {
	require.Empty(t, ParseList(""))
	require.Empty(t, ParseList(" , "))
	require.Equal(t, []string{"a", "b"}, ParseList(" a, b ,"))
}
