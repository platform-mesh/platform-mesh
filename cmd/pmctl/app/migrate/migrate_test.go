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

package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"sigs.k8s.io/yaml"
)

func TestParseVersion(t *testing.T) {
	for in, want := range map[string]string{
		"v26.10.2":                     "26.10.2",
		"26.10.2":                      "26.10.2",
		"v26.10.x":                     "26.10.0",
		"v26.10":                       "26.10.0",
		" v26.11.0":                    "26.11.0",
		"26.11.0-snapshot.12.gabcdef0": "26.11.0-snapshot.12.gabcdef0",
	} {
		got, err := parseVersion(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got.String(), in)
	}
	for _, in := range []string{"", "latest", "v26", "26.x", "v26.10.2.1"} {
		_, err := parseVersion(in)
		require.Error(t, err, in)
	}
}

func TestPending(t *testing.T) {
	migrations := []Migration{
		{Name: "a", Version: "26.11.0"},
		{Name: "b", Version: "27.2.0"},
	}
	for from, want := range map[string][]string{
		"26.10.2": {"a", "b"},
		"26.10.x": {"a", "b"},
		// A build of the tree that becomes 26.11.0 is not there yet.
		"26.11.0-snapshot.3.gabcdef0": {"a", "b"},
		"26.11.0":                     {"b"},
		"26.11.4":                     {"b"},
		"27.2.0":                      nil,
	} {
		v, err := parseVersion(from)
		require.NoError(t, err)
		var got []string
		for _, m := range pending(migrations, v) {
			got = append(got, m.Name)
		}
		require.Equal(t, want, got, from)
	}
}

// The registry itself: names are unique, cut-offs are versions, and the
// order is the order of the cut-offs.
func TestAll(t *testing.T) {
	seen := map[string]bool{}
	var last *semver.Version
	for _, m := range All() {
		require.NotEmpty(t, m.Name)
		require.False(t, seen[m.Name], "migration %s is registered twice", m.Name)
		seen[m.Name] = true
		require.NotEmpty(t, m.Summary, m.Name)
		require.NotNil(t, m.Before, m.Name)

		v, err := semver.StrictNewVersion(m.Version)
		require.NoError(t, err, m.Name)
		if last != nil {
			require.False(t, v.LessThan(last), "migration %s is out of order", m.Name)
		}
		last = v
	}
}

func TestValidate(t *testing.T) {
	o := NewOptions(testStreams())
	require.NoError(t, o.Validate())

	o.Phase = PhaseAfter
	require.ErrorContains(t, o.Validate(), "--from")
	o.From = "v26.10.x"
	require.NoError(t, o.Validate())

	o.From = "latest"
	require.ErrorContains(t, o.Validate(), "--from")
	o.From, o.Phase = "", "during"
	require.ErrorContains(t, o.Validate(), "--phase")
}

// The profile the migrations edit is the one the operator chart ships: the
// edit changes what it is asked to and nothing else.
func TestSetYAMLOnTheKindProfile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "charts", "platform-mesh-operator", "files", "kind-profile.yaml"))
	require.NoError(t, err)
	var cm corev1.ConfigMap
	require.NoError(t, yaml.Unmarshal(data, &cm))
	profile := cm.Data[profileKey]
	require.NotEmpty(t, profile)

	edited, err := setYAML(profile, map[string]any{
		openfgaPath + ".replicaCount":        0,
		openfgaPath + ".autoscaling.enabled": false,
		openfgaPath + ".brand.new.key":       "value",
	})
	require.NoError(t, err)

	var before, after map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(profile), &before))
	require.NoError(t, yaml.Unmarshal([]byte(edited), &after))

	values := []string{"components", "services", "openfga", "values"}
	replicas, found, err := unstructured.NestedFieldNoCopy(after, append(values, "replicaCount")...)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 0, replicas)
	created, _, _ := unstructured.NestedString(after, append(values, "brand", "new", "key")...)
	require.Equal(t, "value", created)

	// Put back what was changed: the rest has to be what it was.
	original, _, _ := unstructured.NestedFieldNoCopy(before, append(values, "replicaCount")...)
	require.NoError(t, unstructured.SetNestedField(after, original, append(values, "replicaCount")...))
	unstructured.RemoveNestedField(after, append(values, "brand")...)
	require.Equal(t, before, after)
}

func TestSetYAML(t *testing.T) {
	doc := "# the head\na:\n  # why b is 1\n  b: 1 # one\n  c: text {{ .template }}\nlist:\n  - x\n"
	got, err := setYAML(doc, map[string]any{"a.b": 2, "d.e": true})
	require.NoError(t, err)
	require.Equal(t, "# the head\na:\n  # why b is 1\n  b: 2 # one\n  c: text {{ .template }}\nlist:\n  - x\nd:\n  e: true\n", got)

	_, err = setYAML(doc, map[string]any{"a.b.c": 1})
	require.ErrorContains(t, err, "a.b is not a mapping")
	_, err = setYAML("- a\n", map[string]any{"a": 1})
	require.Error(t, err)
}

func TestParseOpenFGACounts(t *testing.T) {
	got, err := parseOpenFGACounts("9 12 345")
	require.NoError(t, err)
	require.Equal(t, openfgaCounts{Stores: 9, Models: 12, Tuples: 345}, got)
	_, err = parseOpenFGACounts("")
	require.Error(t, err)
}

func testStreams() genericiooptions.IOStreams {
	return genericiooptions.NewTestIOStreamsDiscard()
}
