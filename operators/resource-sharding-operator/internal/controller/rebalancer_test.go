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

//nolint:goconst
package controller

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmshardingv1alpha1 "go.platform-mesh.io/apis/sharding/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// configMapGVK is the GVK used for ConfigMap-backed rebalancer tests.
// The rebalancer's GVK field is the list GVK; the fake client expects the list kind.
var configMapListGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMapList"}

// newRebalancerScheme returns a scheme that includes the standard client-go types.
func newRebalancerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, pmshardingv1alpha1.AddToScheme(s))
	return s
}

// buildConfigMap creates a ConfigMap labeled with the given shard value.
func buildConfigMap(name, namespace, labelKey, shardValue string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{labelKey: shardValue},
		},
	}
}

// buildConfigMaps creates N uniquely-named ConfigMaps for a given shard.
func buildConfigMaps(prefix, namespace, labelKey, shard string, count int) []ctrlruntimeclient.Object {
	objs := make([]ctrlruntimeclient.Object, 0, count)
	for i := range count {
		name := prefix + shard + "-" + string([]rune{rune('a' + (i/26)%26), rune('a' + i%26)})
		objs = append(objs, buildConfigMap(name, namespace, labelKey, shard))
	}
	return objs
}

// ---------------------------------------------------------------------------
// countPerShard
// ---------------------------------------------------------------------------

func TestRebalancer_CountPerShard_Empty(t *testing.T) {
	scheme := newRebalancerScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &Rebalancer{
		Client:   fc,
		LabelKey: "shard.io/shard",
		GVK:      configMapListGVK,
		Shards:   []string{"a", "b"},
		Config:   pmshardingv1alpha1.RebalanceConfig{},
	}

	counts, err := r.countPerShard(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 0, counts["a"])
	assert.Equal(t, 0, counts["b"])
}

func TestRebalancer_CountPerShard_WithItems(t *testing.T) {
	const labelKey = "shard.io/shard"
	scheme := newRebalancerScheme(t)

	objs := []ctrlruntimeclient.Object{
		buildConfigMap("cm-1", corev1.NamespaceDefault, labelKey, "a"),
		buildConfigMap("cm-2", corev1.NamespaceDefault, labelKey, "a"),
		buildConfigMap("cm-3", corev1.NamespaceDefault, labelKey, "b"),
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	r := &Rebalancer{
		Client:   fc,
		LabelKey: labelKey,
		GVK:      configMapListGVK,
		Shards:   []string{"a", "b"},
		Config:   pmshardingv1alpha1.RebalanceConfig{},
	}

	counts, err := r.countPerShard(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, counts["a"])
	assert.Equal(t, 1, counts["b"])
}

// ---------------------------------------------------------------------------
// cleanupOrphans
// ---------------------------------------------------------------------------

func TestRebalancer_CleanupOrphans_NoOrphans(t *testing.T) {
	const labelKey = "shard.io/shard"
	scheme := newRebalancerScheme(t)

	objs := []ctrlruntimeclient.Object{
		buildConfigMap("cm-1", corev1.NamespaceDefault, labelKey, "valid-a"),
		buildConfigMap("cm-2", corev1.NamespaceDefault, labelKey, "valid-b"),
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	r := &Rebalancer{
		Client:               fc,
		LabelKey:             labelKey,
		GVK:                  configMapListGVK,
		Shards:               []string{"valid-a", "valid-b"},
		Config:               pmshardingv1alpha1.RebalanceConfig{RateLimit: 100},
		ResourceShardingName: "test-rs",
	}

	n, err := r.cleanupOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "no orphans should be removed when all labels are valid")
}

func TestRebalancer_CleanupOrphans_StripsOrphanLabel(t *testing.T) {
	const labelKey = "shard.io/shard"
	scheme := newRebalancerScheme(t)

	objs := []ctrlruntimeclient.Object{
		buildConfigMap("orphan-1", corev1.NamespaceDefault, labelKey, "deleted-shard"),
		buildConfigMap("valid-1", corev1.NamespaceDefault, labelKey, "shard-a"),
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	r := &Rebalancer{
		Client:               fc,
		LabelKey:             labelKey,
		GVK:                  configMapListGVK,
		Shards:               []string{"shard-a"},
		Config:               pmshardingv1alpha1.RebalanceConfig{RateLimit: 100},
		ResourceShardingName: "test-rs",
	}

	n, err := r.cleanupOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "orphan resource should have been cleaned up")
}

// ---------------------------------------------------------------------------
// assignUnlabeled
// ---------------------------------------------------------------------------

func TestRebalancer_AssignUnlabeled_AssignsToLeastLoaded(t *testing.T) {
	const labelKey = "shard.io/shard"
	scheme := newRebalancerScheme(t)

	objs := []ctrlruntimeclient.Object{
		buildConfigMap("labeled-a-1", corev1.NamespaceDefault, labelKey, "shard-a"),
		buildConfigMap("labeled-a-2", corev1.NamespaceDefault, labelKey, "shard-a"),
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "no-label", Namespace: corev1.NamespaceDefault}},
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	r := &Rebalancer{
		Client:               fc,
		LabelKey:             labelKey,
		GVK:                  configMapListGVK,
		Shards:               []string{"shard-a", "shard-b"},
		Config:               pmshardingv1alpha1.RebalanceConfig{RateLimit: 100},
		ResourceShardingName: "test-rs",
	}

	counts := map[string]int{"shard-a": 2, "shard-b": 0}
	n, err := r.assignUnlabeled(context.Background(), counts)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the single unlabeled object should be assigned")
	assert.Equal(t, 1, counts["shard-b"], "least-loaded shard-b should receive the assignment")
	assert.Equal(t, 2, counts["shard-a"], "shard-a count should be unchanged")

	var got corev1.ConfigMap
	require.NoError(t, fc.Get(context.Background(), ctrlruntimeclient.ObjectKey{Name: "no-label", Namespace: corev1.NamespaceDefault}, &got))
	assert.Equal(t, "shard-b", got.Labels[labelKey])
}

func TestRebalancer_AssignUnlabeled_NoUnlabeledIsNoOp(t *testing.T) {
	const labelKey = "shard.io/shard"
	scheme := newRebalancerScheme(t)

	objs := []ctrlruntimeclient.Object{
		buildConfigMap("labeled-a-1", corev1.NamespaceDefault, labelKey, "shard-a"),
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	r := &Rebalancer{
		Client:               fc,
		LabelKey:             labelKey,
		GVK:                  configMapListGVK,
		Shards:               []string{"shard-a", "shard-b"},
		Config:               pmshardingv1alpha1.RebalanceConfig{RateLimit: 100},
		ResourceShardingName: "test-rs",
	}

	counts := map[string]int{"shard-a": 1, "shard-b": 0}
	n, err := r.assignUnlabeled(context.Background(), counts)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, counts["shard-b"], "no unlabeled objects should mean no assignment")
}

func TestRebalancer_AssignUnlabeled_NoShardsIsNoOp(t *testing.T) {
	const labelKey = "shard.io/shard"
	scheme := newRebalancerScheme(t)

	objs := []ctrlruntimeclient.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "no-label", Namespace: corev1.NamespaceDefault}},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	r := &Rebalancer{
		Client:               fc,
		LabelKey:             labelKey,
		GVK:                  configMapListGVK,
		Shards:               nil,
		Config:               pmshardingv1alpha1.RebalanceConfig{RateLimit: 100},
		ResourceShardingName: "test-rs",
	}

	n, err := r.assignUnlabeled(context.Background(), map[string]int{})
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestLeastLoaded(t *testing.T) {
	tests := []struct {
		name   string
		shards []string
		counts map[string]int
		want   string
	}{
		{name: "empty shards", shards: nil, counts: map[string]int{}, want: ""},
		{name: "single shard", shards: []string{"a"}, counts: map[string]int{"a": 5}, want: "a"},
		{name: "first least loaded", shards: []string{"a", "b"}, counts: map[string]int{"a": 1, "b": 2}, want: "a"},
		{name: "second least loaded", shards: []string{"a", "b"}, counts: map[string]int{"a": 2, "b": 1}, want: "b"},
		{name: "ties favor first", shards: []string{"a", "b"}, counts: map[string]int{"a": 1, "b": 1}, want: "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, leastLoaded(tt.shards, tt.counts))
		})
	}
}

// ---------------------------------------------------------------------------
// rebalance (move math)
// ---------------------------------------------------------------------------

func TestRebalancer_Rebalance_Math(t *testing.T) {
	tests := []struct {
		name        string
		shards      []string
		counts      map[string]int
		cfg         pmshardingv1alpha1.RebalanceConfig
		wantMoved   int
		wantAtLeast int // if > 0, assert moved >= wantAtLeast
	}{
		{
			name:      "balanced — no moves",
			shards:    []string{"a", "b"},
			counts:    map[string]int{"a": 10, "b": 10},
			cfg:       pmshardingv1alpha1.RebalanceConfig{Threshold: 20, MovesPerCycle: 10, MinMovesPerCycle: 1, RateLimit: 100},
			wantMoved: 0,
		},
		{
			name:      "zero total — no moves",
			shards:    []string{"a", "b"},
			counts:    map[string]int{"a": 0, "b": 0},
			cfg:       pmshardingv1alpha1.RebalanceConfig{Threshold: 20, MovesPerCycle: 10, MinMovesPerCycle: 1, RateLimit: 100},
			wantMoved: 0,
		},
		{
			name:      "no shards — no moves",
			shards:    []string{},
			counts:    map[string]int{},
			cfg:       pmshardingv1alpha1.RebalanceConfig{Threshold: 20, MovesPerCycle: 10, MinMovesPerCycle: 1, RateLimit: 100},
			wantMoved: 0,
		},
		{
			name:   "just at threshold — no moves",
			shards: []string{"a", "b"},
			// ideal=10, threshold=20 → maxAllowed=12; shard-a=12 is NOT >12 → no rebalance
			counts:    map[string]int{"a": 12, "b": 8},
			cfg:       pmshardingv1alpha1.RebalanceConfig{Threshold: 20, MovesPerCycle: 10, MinMovesPerCycle: 1, RateLimit: 100},
			wantMoved: 0,
		},
		{
			name:   "one over threshold — moves triggered",
			shards: []string{"a", "b"},
			// ideal=10, threshold=20 → maxAllowed=12; shard-a=15 > 12 → overloaded
			counts:      map[string]int{"a": 15, "b": 5},
			cfg:         pmshardingv1alpha1.RebalanceConfig{Threshold: 20, MovesPerCycle: 10, MinMovesPerCycle: 1, RateLimit: 100},
			wantAtLeast: 1,
		},
		{
			name:   "minMovesPerCycle floor applied",
			shards: []string{"a", "b"},
			// ideal=15, threshold=20 → maxAllowed=18; shard-a=30 >> maxAllowed
			// toMove=15; movesPercent=10 → maxMoves=1; minMoves=5 → floor to 5
			counts:      map[string]int{"a": 30, "b": 0},
			cfg:         pmshardingv1alpha1.RebalanceConfig{Threshold: 20, MovesPerCycle: 10, MinMovesPerCycle: 5, RateLimit: 100},
			wantAtLeast: 5,
		},
		{
			name:   "single shard — no underloaded target",
			shards: []string{"a"},
			counts: map[string]int{"a": 50},
			cfg:    pmshardingv1alpha1.RebalanceConfig{Threshold: 20, MovesPerCycle: 10, MinMovesPerCycle: 1, RateLimit: 100},
			// ideal == total, so no shard is ever "overloaded" above maxAllowed
			wantMoved: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const labelKey = "shard.io/shard"
			scheme := newRebalancerScheme(t)

			// Pre-populate the fake client with ConfigMap objects matching counts
			total := 0
			for _, c := range tc.counts {
				total += c
			}
			initObjs := make([]ctrlruntimeclient.Object, 0, total)
			objIdx := 0
			for shard, count := range tc.counts {
				for range count {
					objIdx++
					name := "cm-" + shard + "-" + string([]rune{rune('a' + (objIdx/26)%26), rune('a' + objIdx%26)})
					initObjs = append(initObjs, buildConfigMap(name, corev1.NamespaceDefault, labelKey, shard))
				}
			}

			fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(initObjs...).Build()

			r := &Rebalancer{
				Client:               fc,
				LabelKey:             labelKey,
				GVK:                  configMapListGVK,
				Shards:               tc.shards,
				Config:               tc.cfg,
				ResourceShardingName: "test-rs",
			}

			// Clone counts since rebalance mutates the map in-place
			countsCopy := maps.Clone(tc.counts)

			moved, err := r.rebalance(context.Background(), countsCopy)
			require.NoError(t, err)

			if tc.wantAtLeast > 0 {
				assert.GreaterOrEqual(t, moved, tc.wantAtLeast,
					"expected at least %d moves, got %d", tc.wantAtLeast, moved)
			} else {
				assert.Equal(t, tc.wantMoved, moved)
			}
		})
	}
}

func TestRebalancer_Rebalance_MovesCapByToMove(t *testing.T) {
	// Verify maxMoves is capped by toMove (never moves more than the excess).
	// ideal=5, threshold=20 → maxAllowed=6; shard-a=8 → overloaded, toMove=3
	// MinMovesPerCycle=100 would try to set maxMoves=100 but toMove=3 caps it to 3.
	const labelKey = "shard.io/shard"
	scheme := newRebalancerScheme(t)

	initObjs := append(
		buildConfigMaps("cm-", corev1.NamespaceDefault, labelKey, "shard-a", 8),
		buildConfigMaps("cm-", corev1.NamespaceDefault, labelKey, "shard-b", 2)...,
	)

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(initObjs...).Build()

	r := &Rebalancer{
		Client:               fc,
		LabelKey:             labelKey,
		GVK:                  configMapListGVK,
		Shards:               []string{"shard-a", "shard-b"},
		Config:               pmshardingv1alpha1.RebalanceConfig{Threshold: 20, MovesPerCycle: 10, MinMovesPerCycle: 100, RateLimit: 100},
		ResourceShardingName: "test-rs",
	}

	counts := map[string]int{"shard-a": 8, "shard-b": 2}
	moved, err := r.rebalance(context.Background(), counts)
	require.NoError(t, err)

	// toMove = 8-5 = 3; maxMoves = min(100, 3) = 3
	assert.LessOrEqual(t, moved, 3, "should never move more than the total excess")
}

// ---------------------------------------------------------------------------
// rateLimit helper
// ---------------------------------------------------------------------------

func TestRebalancer_RateLimit_DefaultsTo10(t *testing.T) {
	r := &Rebalancer{Config: pmshardingv1alpha1.RebalanceConfig{}}
	assert.Equal(t, 10, r.rateLimit())
}

func TestRebalancer_RateLimit_UsesConfigValue(t *testing.T) {
	r := &Rebalancer{Config: pmshardingv1alpha1.RebalanceConfig{RateLimit: 50}}
	assert.Equal(t, 50, r.rateLimit())
}

// ---------------------------------------------------------------------------
// Run (integration of countPerShard + cleanupOrphans + rebalance)
// ---------------------------------------------------------------------------

func TestRebalancer_Run_AllBalanced(t *testing.T) {
	const labelKey = "shard.io/shard"
	scheme := newRebalancerScheme(t)

	objs := []ctrlruntimeclient.Object{
		buildConfigMap("cm-1", corev1.NamespaceDefault, labelKey, "shard-a"),
		buildConfigMap("cm-2", corev1.NamespaceDefault, labelKey, "shard-b"),
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	r := &Rebalancer{
		Client:               fc,
		LabelKey:             labelKey,
		GVK:                  configMapListGVK,
		Shards:               []string{"shard-a", "shard-b"},
		Config:               pmshardingv1alpha1.RebalanceConfig{Threshold: 20, MovesPerCycle: 10, MinMovesPerCycle: 1, RateLimit: 100},
		ResourceShardingName: "test-rs",
	}

	result, err := r.Run(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 0, result.Moved, "no moves expected in balanced state")
	assert.Len(t, result.Distribution, 2, "distribution should have one entry per shard")
}

func TestRebalancer_Run_EmptyCluster(t *testing.T) {
	const labelKey = "shard.io/shard"
	scheme := newRebalancerScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &Rebalancer{
		Client:               fc,
		LabelKey:             labelKey,
		GVK:                  configMapListGVK,
		Shards:               []string{"shard-a", "shard-b"},
		Config:               pmshardingv1alpha1.RebalanceConfig{Threshold: 20, MovesPerCycle: 10, MinMovesPerCycle: 1, RateLimit: 100},
		ResourceShardingName: "test-rs",
	}

	result, err := r.Run(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 0, result.Moved)
	assert.Len(t, result.Distribution, 2)
	for _, d := range result.Distribution {
		assert.Equal(t, 0, d.Count)
	}
}
