package backup

import (
	"testing"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAuthoritativeEtcdShardsExcludesEtcdCache(t *testing.T) {
	shards := []druidv1alpha1.Etcd{
		{ObjectMeta: metav1.ObjectMeta{Name: "root"}},
		{ObjectMeta: metav1.ObjectMeta{Name: etcdCacheName}},
		{ObjectMeta: metav1.ObjectMeta{Name: "shard-1"}},
	}

	authoritative := authoritativeEtcdShards(shards)
	if len(authoritative) != 2 {
		t.Fatalf("expected 2 authoritative shards, got %d", len(authoritative))
	}
	if authoritative[0].Name != "root" || authoritative[1].Name != "shard-1" {
		t.Fatalf("unexpected authoritative shards: %q, %q", authoritative[0].Name, authoritative[1].Name)
	}
}
