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

package app

import (
	"testing"

	krofeatures "github.com/kubernetes-sigs/kro/pkg/features"
	"github.com/stretchr/testify/require"
)

// The gates the operator exposes are kro's, read from kro's global gate. EnabledFeatures is
// what reports them at startup, so it has to list what is on rather than what is known.
func TestEnabledFeatures(t *testing.T) {
	require.Empty(t, EnabledFeatures(), "every kro gate defaults to off")

	require.NoError(t, krofeatures.FeatureGate.Set("InstanceConditionEvents=true"))
	require.Equal(t, []string{"InstanceConditionEvents"}, EnabledFeatures())

	require.NoError(t, krofeatures.FeatureGate.Set("InstanceConditionEvents=false"))
	require.Empty(t, EnabledFeatures())

	require.Error(t, krofeatures.FeatureGate.Set("Bogus=true"),
		"an unknown gate must fail rather than be silently ignored")
}

func TestStripClusters(t *testing.T) {
	t.Parallel()
	require.Equal(t, "https://kcp.example.com:6443",
		StripClusters("https://kcp.example.com:6443/clusters/root:providers:kro-provider"))
	require.Equal(t, "https://kcp.example.com:6443",
		StripClusters("https://kcp.example.com:6443/"))
	require.Equal(t, "https://kcp.example.com:6443",
		StripClusters("https://kcp.example.com:6443"))
}
