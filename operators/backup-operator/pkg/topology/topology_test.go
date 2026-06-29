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

package topology_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	operatorcfg "go.platform-mesh.io/backup-operator/pkg/config"
	"go.platform-mesh.io/backup-operator/pkg/topology"
)

// rfcSampleDigest is a valid sha256 digest used in the RFC 009 sample document.
const rfcSampleDigest = "sha256:a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3"

// sampleManifest returns a fully-populated Manifest matching the RFC 009 sample.
func sampleManifest() *topology.Manifest {
	return &topology.Manifest{
		SchemaVersion: "v1alpha1",
		CapturedAt:    time.Date(2026, 5, 20, 13, 4, 22, 0, time.UTC),
		HostCluster: topology.HostCluster{
			KubernetesVersion: "v1.32.4",
			Namespace:         operatorcfg.DefaultNamespace,
		},
		Kcp: topology.KcpTopology{
			ShardCount: 2,
			Shards: []topology.KcpShard{
				{Name: "root", EtcdRef: "etcd/root", LogicalClusterIDsDigest: rfcSampleDigest},
				{Name: "shard-a", EtcdRef: "etcd/shard-a", LogicalClusterIDsDigest: rfcSampleDigest},
			},
		},
		CNPG: topology.CNPGTopology{
			Clusters: []topology.CNPGCluster{
				{Name: "openfga-db", SpecDigest: rfcSampleDigest, MajorVersion: 16},
				{Name: "keycloak-db", SpecDigest: rfcSampleDigest, MajorVersion: 16},
			},
		},
		OpenFGA: topology.OpenFGATopology{
			Stores: []topology.OpenFGAStore{
				{Name: "orgs", ModelDigest: rfcSampleDigest},
			},
		},
		OperatorVersion: "0.1.0-poc",
	}
}

// TestMarshalUnmarshalRoundTrip verifies that marshalling a Manifest to JSON and unmarshalling it back produces an identical struct.
func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	original := sampleManifest()

	data, err := topology.Marshal(original)
	require.NoError(t, err)

	var got topology.Manifest
	require.NoError(t, topology.Unmarshal(data, &got))

	assert.Equal(t, original.SchemaVersion, got.SchemaVersion)
	assert.Equal(t, original.OperatorVersion, got.OperatorVersion)
	assert.True(t, original.CapturedAt.Equal(got.CapturedAt))
	assert.Equal(t, original.HostCluster, got.HostCluster)
	assert.Equal(t, original.Kcp, got.Kcp)
	assert.Equal(t, original.CNPG, got.CNPG)
	assert.Equal(t, original.OpenFGA, got.OpenFGA)
}

// TestUnmarshalMissingRequiredField verifies that Unmarshal rejects a JSON document that omits a required field and returns a ValidationError.
func TestUnmarshalMissingRequiredField(t *testing.T) {
	doc := map[string]any{
		// schemaVersion deliberately omitted
		"capturedAt": "2026-05-20T13:04:22Z",
		"hostCluster": map[string]any{
			"kubernetesVersion": "v1.32.4",
			"namespace":         operatorcfg.DefaultNamespace,
		},
		"kcp": map[string]any{
			"shardCount": 1,
			"shards": []any{
				map[string]any{
					"name":                    "root",
					"etcdRef":                 "etcd/root",
					"logicalClusterIDsDigest": rfcSampleDigest,
				},
			},
		},
		"cnpg":            map[string]any{"clusters": []any{}},
		"openfga":         map[string]any{"stores": []any{}},
		"operatorVersion": "0.1.0-poc",
	}
	data, _ := json.Marshal(doc)

	var m topology.Manifest
	err := topology.Unmarshal(data, &m)
	require.Error(t, err)

	var ve *topology.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.NotEmpty(t, ve.SchemaErrors)
}

// TestUnmarshalBadDigest verifies that Unmarshal rejects a document containing a malformed sha256 digest and returns a ValidationError.
func TestUnmarshalBadDigest(t *testing.T) {
	doc := map[string]any{
		"schemaVersion": "v1alpha1",
		"capturedAt":    "2026-05-20T13:04:22Z",
		"hostCluster": map[string]any{
			"kubernetesVersion": "v1.32.4",
			"namespace":         operatorcfg.DefaultNamespace,
		},
		"kcp": map[string]any{
			"shardCount": 1,
			"shards": []any{
				map[string]any{
					"name":                    "root",
					"etcdRef":                 "etcd/root",
					"logicalClusterIDsDigest": "not-a-sha256", // bad
				},
			},
		},
		"cnpg":            map[string]any{"clusters": []any{}},
		"openfga":         map[string]any{"stores": []any{}},
		"operatorVersion": "0.1.0-poc",
	}
	data, _ := json.Marshal(doc)

	var m topology.Manifest
	err := topology.Unmarshal(data, &m)
	require.Error(t, err)

	var ve *topology.ValidationError
	require.ErrorAs(t, err, &ve)
}

// TestValidateIdentical verifies that Validate returns nil when source and target manifests are identical.
func TestValidateIdentical(t *testing.T) {
	m := sampleManifest()
	require.NoError(t, topology.Validate(m, m))
}

// TestValidateShardDigestMismatch verifies that Validate returns a MismatchError identifying the field when one shard's logicalClusterIDsDigest differs between source and target.
func TestValidateShardDigestMismatch(t *testing.T) {
	source := sampleManifest()
	target := sampleManifest()
	target.Kcp.Shards[0].LogicalClusterIDsDigest = "sha256:" + "b" + rfcSampleDigest[8:]

	err := topology.Validate(source, target)
	require.Error(t, err)

	var me *topology.MismatchError
	require.ErrorAs(t, err, &me)
	require.Len(t, me.Fields, 1)
	assert.Equal(t, "kcp.shards[root].logicalClusterIDsDigest", me.Fields[0].Field)
}

// TestValidateExtraShardOnTarget verifies that Validate returns a MismatchError when the target manifest contains a shard not present in the source.
func TestValidateExtraShardOnTarget(t *testing.T) {
	source := sampleManifest()
	target := sampleManifest()
	target.Kcp.Shards = append(target.Kcp.Shards, topology.KcpShard{
		Name:                    "shard-b",
		EtcdRef:                 "etcd/shard-b",
		LogicalClusterIDsDigest: rfcSampleDigest,
	})

	err := topology.Validate(source, target)
	require.Error(t, err)

	var me *topology.MismatchError
	require.ErrorAs(t, err, &me)
	found := false
	for _, f := range me.Fields {
		if f.Field == "kcp.shards[shard-b]" {
			found = true
		}
	}
	assert.True(t, found, "expected mismatch for extra shard shard-b")
}

// TestDigestStable verifies that Digest returns a stable, non-empty sha256 hex string across multiple calls on the same Manifest.
func TestDigestStable(t *testing.T) {
	m := sampleManifest()

	d1, err := topology.Digest(m)
	require.NoError(t, err)
	d2, err := topology.Digest(m)
	require.NoError(t, err)

	assert.Equal(t, d1, d2)
	assert.True(t, len(d1) > 0)
	assert.Contains(t, d1, "sha256:")
}

// TestRFC009SampleDocument verifies that the canonical RFC 009 sample JSON document passes Unmarshal and validates against itself without error.
func TestRFC009SampleDocument(t *testing.T) {
	raw := `{
		"schemaVersion": "v1alpha1",
		"capturedAt": "2026-05-20T13:04:22Z",
		"hostCluster": {
			"kubernetesVersion": "v1.32.4",
			"namespace": "platform-mesh"
		},
		"kcp": {
			"shardCount": 2,
			"shards": [
				{ "name": "root",    "etcdRef": "etcd/root",    "logicalClusterIDsDigest": "` + rfcSampleDigest + `" },
				{ "name": "shard-a", "etcdRef": "etcd/shard-a", "logicalClusterIDsDigest": "` + rfcSampleDigest + `" }
			]
		},
		"cnpg": {
			"clusters": [
				{ "name": "openfga-db",  "specDigest": "` + rfcSampleDigest + `", "majorVersion": 16 },
				{ "name": "keycloak-db", "specDigest": "` + rfcSampleDigest + `", "majorVersion": 16 }
			]
		},
		"openfga": {
			"stores": [ { "name": "orgs", "modelDigest": "` + rfcSampleDigest + `" } ]
		},
		"operatorVersion": "0.1.0-poc"
	}`

	var m topology.Manifest
	require.NoError(t, topology.Unmarshal([]byte(raw), &m))
	require.NoError(t, topology.Validate(&m, &m))
}

// TestValidateExtraCNPGClusterOnTarget verifies that Validate detects a CNPG cluster present on the target but absent from the source and reports it as a MismatchError.
func TestValidateExtraCNPGClusterOnTarget(t *testing.T) {
	source := sampleManifest()
	source.CNPG.Clusters = []topology.CNPGCluster{
		{Name: "db-a", SpecDigest: rfcSampleDigest, MajorVersion: 16},
	}
	target := sampleManifest()
	target.CNPG.Clusters = []topology.CNPGCluster{
		{Name: "db-a", SpecDigest: rfcSampleDigest, MajorVersion: 16},
		{Name: "db-extra", SpecDigest: rfcSampleDigest, MajorVersion: 16}, // not in source
	}

	err := topology.Validate(source, target)
	require.Error(t, err, "extra CNPG cluster on target must be a mismatch")
	me, ok := err.(*topology.MismatchError)
	require.True(t, ok)
	found := false
	for _, f := range me.Fields {
		if f.Field == "cnpg.clusters[db-extra]" && f.Source == "<missing>" {
			found = true
		}
	}
	assert.True(t, found, "expected mismatch for extra CNPG cluster db-extra")
}

// TestValidateExtraOpenFGAStoreOnTarget verifies that Validate detects an OpenFGA store present on the target but absent from the source and reports it as a MismatchError.
func TestValidateExtraOpenFGAStoreOnTarget(t *testing.T) {
	source := sampleManifest()
	source.OpenFGA.Stores = []topology.OpenFGAStore{
		{Name: "store-a", ModelDigest: rfcSampleDigest},
	}
	target := sampleManifest()
	target.OpenFGA.Stores = []topology.OpenFGAStore{
		{Name: "store-a", ModelDigest: rfcSampleDigest},
		{Name: "store-extra", ModelDigest: rfcSampleDigest}, // not in source
	}

	err := topology.Validate(source, target)
	require.Error(t, err, "extra OpenFGA store on target must be a mismatch")
	me, ok := err.(*topology.MismatchError)
	require.True(t, ok)
	found := false
	for _, f := range me.Fields {
		if f.Field == "openfga.stores[store-extra]" && f.Source == "<missing>" {
			found = true
		}
	}
	assert.True(t, found, "expected mismatch for extra OpenFGA store store-extra")
}

// TestValidateCNPGMajorVersionMismatch verifies that Validate detects a Postgres major version difference between source and target CNPG clusters and reports it as a MismatchError.
func TestValidateCNPGMajorVersionMismatch(t *testing.T) {
	source := sampleManifest()
	source.CNPG.Clusters = []topology.CNPGCluster{
		{Name: "db-a", SpecDigest: rfcSampleDigest, MajorVersion: 15},
	}
	target := sampleManifest()
	target.CNPG.Clusters = []topology.CNPGCluster{
		{Name: "db-a", SpecDigest: rfcSampleDigest, MajorVersion: 16}, // upgraded
	}

	err := topology.Validate(source, target)
	require.Error(t, err, "Postgres major version upgrade must be a mismatch")
	me, ok := err.(*topology.MismatchError)
	require.True(t, ok)
	found := false
	for _, f := range me.Fields {
		if f.Field == "cnpg.clusters[db-a].majorVersion" && f.Source == "15" && f.Target == "16" {
			found = true
		}
	}
	assert.True(t, found, "expected majorVersion mismatch for db-a (15→16)")
}

// TestValidateDuplicateShardNames verifies that duplicate shard names in the source manifest cause Validate to return an error rather than silently corrupting the comparison.
func TestValidateDuplicateShardNames(t *testing.T) {
	source := sampleManifest()
	source.Kcp.Shards = []topology.KcpShard{
		{Name: "shard-a", EtcdRef: "etcd/a", LogicalClusterIDsDigest: rfcSampleDigest},
		{Name: "shard-a", EtcdRef: "etcd/a-copy", LogicalClusterIDsDigest: rfcSampleDigest}, // duplicate
	}
	target := sampleManifest()
	target.Kcp.Shards = []topology.KcpShard{
		{Name: "shard-a", EtcdRef: "etcd/different", LogicalClusterIDsDigest: rfcSampleDigest},
	}

	// Duplicate names produce a <duplicate> sentinel on the source side, which will
	// never match the target's actual value — so Validate must return a mismatch.
	err := topology.Validate(source, target)
	require.Error(t, err, "duplicate shard names in source must produce a mismatch, not silent corruption")
}
