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

// a. Marshal → Unmarshal round-trip returns identical struct.
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

// b. Unmarshal rejects a document missing a required field.
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

// c. Unmarshal rejects a document with a malformed sha256 digest.
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

// d. Validate returns nil when source and target are identical.
func TestValidateIdentical(t *testing.T) {
	m := sampleManifest()
	require.NoError(t, topology.Validate(m, m))
}

// e. Validate returns *MismatchError when one shard digest differs.
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

// f. Validate returns *MismatchError when shard name sets differ.
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

// g. Digest returns a stable hex string across two calls.
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

// h. RFC 009 sample document passes Unmarshal and Validate(sample, sample).
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
