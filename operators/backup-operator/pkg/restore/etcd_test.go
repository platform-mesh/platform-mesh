package restore

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestEtcdRestoreShardNamesReturnsAuthoritativeShardsOnly(t *testing.T) {
	state := &corev1.ConfigMap{Data: map[string]string{
		etcdRestoreStateShardsKey: "root, " + etcdCacheName + ", shard-1",
	}}

	want := []string{"root", "shard-1"}
	if got := etcdRestoreShardNames(state); !reflect.DeepEqual(got, want) {
		t.Fatalf("etcdRestoreShardNames() = %v, want %v", got, want)
	}
}

func TestEtcdRestoreStateKeepsCacheManifestAndPhaseSeparate(t *testing.T) {
	state := &corev1.ConfigMap{Data: map[string]string{
		etcdRestoreStateShardsKey:             "etcd-kcp,nereus-shard-kcp,triton-shard-kcp",
		etcdRestoreManifestKey(etcdCacheName): `{"apiVersion":"druid.gardener.cloud/v1alpha1","kind":"Etcd","metadata":{"name":"etcd-cache","namespace":"platform-mesh-system"}}`,
		etcdRestorePhaseKey(etcdCacheName):    etcdCacheRestorePhaseCaptured,
		etcdRestorePhaseKey("etcd-kcp"):       etcdRestorePhaseCaptured,
		etcdRestoreStateCompletedKey:          "false",
	}}

	if got := etcdRestoreShardNames(state); !reflect.DeepEqual(got, []string{"etcd-kcp", "nereus-shard-kcp", "triton-shard-kcp"}) {
		t.Fatalf("etcdRestoreShardNames() = %v, want authoritative shards only", got)
	}
	if got := state.Data[etcdRestorePhaseKey(etcdCacheName)]; got != etcdCacheRestorePhaseCaptured {
		t.Fatalf("cache phase = %q, want %q", got, etcdCacheRestorePhaseCaptured)
	}

	cache, err := etcdFromRestoreState(state, etcdCacheName)
	if err != nil {
		t.Fatalf("etcdFromRestoreState(cache): %v", err)
	}
	if cache.GetName() != etcdCacheName {
		t.Fatalf("cache manifest name = %q, want %q", cache.GetName(), etcdCacheName)
	}
}
