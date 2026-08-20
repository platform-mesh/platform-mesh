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
