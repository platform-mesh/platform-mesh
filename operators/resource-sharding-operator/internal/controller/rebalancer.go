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

package controller

import (
	"context"
	"fmt"

	"go.platform-mesh.io/apis/sharding/v1alpha1"
	"golang.org/x/time/rate"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type RebalanceResult struct {
	Distribution []v1alpha1.ShardDistribution
	Moved        int
}

type Rebalancer struct {
	Client               client.Client
	LabelKey             string
	GVK                  schema.GroupVersionKind
	Shards               []string
	Config               v1alpha1.RebalanceConfig
	ResourceShardingName string
}

func (r *Rebalancer) Run(ctx context.Context) (*RebalanceResult, error) {
	logger := log.FromContext(ctx)

	orphanCount, err := r.cleanupOrphans(ctx)
	if err != nil {
		logger.Error(err, "orphan cleanup failed")
	}

	counts, err := r.countPerShard(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting shards: %w", err)
	}

	assigned, err := r.assignUnlabeled(ctx, counts)
	if err != nil {
		logger.Error(err, "unlabeled assignment failed")
	}

	moved, err := r.rebalance(ctx, counts)
	if err != nil {
		logger.Error(err, "rebalance moves failed")
	}

	distribution := make([]v1alpha1.ShardDistribution, 0, len(r.Shards))
	total := 0
	for _, shard := range r.Shards {
		distribution = append(distribution, v1alpha1.ShardDistribution{
			Shard: shard,
			Count: counts[shard],
		})
		total += counts[shard]
		shardDistribution.WithLabelValues(r.ResourceShardingName, shard).Set(float64(counts[shard]))
	}

	if total > 0 && len(r.Shards) > 0 {
		ideal := float64(total) / float64(len(r.Shards))
		maxDeviation := 0.0
		for _, c := range counts {
			dev := (float64(c) - ideal) / ideal
			if dev < 0 {
				dev = -dev
			}
			if dev > maxDeviation {
				maxDeviation = dev
			}
		}
		shardImbalanceRatio.WithLabelValues(r.ResourceShardingName).Set(maxDeviation)
	}

	totalMoved := moved + orphanCount + assigned
	if totalMoved > 0 {
		rebalanceMovesTotal.WithLabelValues(r.ResourceShardingName).Add(float64(totalMoved))
	}

	return &RebalanceResult{
		Distribution: distribution,
		Moved:        totalMoved,
	}, nil
}

func (r *Rebalancer) countPerShard(ctx context.Context) (map[string]int, error) {
	counts := make(map[string]int, len(r.Shards))

	for _, shard := range r.Shards {
		selector, err := labels.Parse(r.LabelKey + "=" + shard)
		if err != nil {
			return nil, err
		}

		list := &metav1.PartialObjectMetadataList{}
		list.SetGroupVersionKind(r.GVK)

		if err := r.Client.List(ctx, list,
			client.MatchingLabelsSelector{Selector: selector},
			client.Limit(1),
		); err != nil {
			return nil, fmt.Errorf("listing shard %s: %w", shard, err)
		}

		count := len(list.Items)
		if list.RemainingItemCount != nil {
			count += int(*list.RemainingItemCount)
		}
		counts[shard] = count
	}

	return counts, nil
}

func (r *Rebalancer) cleanupOrphans(ctx context.Context) (int, error) {
	selector, err := labels.Parse(r.LabelKey)
	if err != nil {
		return 0, err
	}

	list := &metav1.PartialObjectMetadataList{}
	list.SetGroupVersionKind(r.GVK)

	if err := r.Client.List(ctx, list,
		client.MatchingLabelsSelector{Selector: selector},
		client.Limit(100),
	); err != nil {
		return 0, err
	}

	validShards := make(map[string]struct{}, len(r.Shards))
	for _, s := range r.Shards {
		validShards[s] = struct{}{}
	}

	limiter := rate.NewLimiter(rate.Limit(r.rateLimit()), 1)
	orphanCount := 0

	for i := range list.Items {
		item := &list.Items[i]
		labelValue := item.Labels[r.LabelKey]
		if _, valid := validShards[labelValue]; valid {
			continue
		}

		if err := limiter.Wait(ctx); err != nil {
			return orphanCount, err
		}

		patch := client.MergeFrom(item.DeepCopy())
		delete(item.Labels, r.LabelKey)
		if err := r.Client.Patch(ctx, item, patch); err != nil {
			return orphanCount, fmt.Errorf("stripping orphan label: %w", err)
		}
		orphanCount++
	}

	return orphanCount, nil
}

// assignUnlabeled finds objects matching the target GVK that lack the shard
// label and assigns each to the currently least-loaded shard. This is the
// deterministic backstop for the dynamic controller's watch-based assignment:
// any object the watch missed (or that loses its label later) is picked up on
// the next reconcile cycle.
func (r *Rebalancer) assignUnlabeled(ctx context.Context, counts map[string]int) (int, error) {
	if len(r.Shards) == 0 {
		return 0, nil
	}

	selector, err := labels.Parse("!" + r.LabelKey)
	if err != nil {
		return 0, err
	}

	list := &metav1.PartialObjectMetadataList{}
	list.SetGroupVersionKind(r.GVK)

	if err := r.Client.List(ctx, list,
		client.MatchingLabelsSelector{Selector: selector},
		client.Limit(100),
	); err != nil {
		return 0, err
	}

	limiter := rate.NewLimiter(rate.Limit(r.rateLimit()), 1)
	assigned := 0

	for i := range list.Items {
		item := &list.Items[i]

		shard := leastLoaded(r.Shards, counts)
		if shard == "" {
			break
		}

		if err := limiter.Wait(ctx); err != nil {
			return assigned, err
		}

		patch := client.MergeFrom(item.DeepCopy())
		if item.Labels == nil {
			item.Labels = make(map[string]string)
		}
		item.Labels[r.LabelKey] = shard
		if err := r.Client.Patch(ctx, item, patch); err != nil {
			return assigned, fmt.Errorf("assigning shard to unlabeled resource: %w", err)
		}
		counts[shard]++
		assignmentsTotal.WithLabelValues(r.ResourceShardingName, shard).Inc()
		assigned++
	}

	return assigned, nil
}

func leastLoaded(shards []string, counts map[string]int) string {
	if len(shards) == 0 {
		return ""
	}
	best := shards[0]
	for _, s := range shards[1:] {
		if counts[s] < counts[best] {
			best = s
		}
	}
	return best
}

func (r *Rebalancer) rebalance(ctx context.Context, counts map[string]int) (int, error) {
	total := 0
	for _, c := range counts {
		total += c
	}
	if total == 0 || len(r.Shards) == 0 {
		return 0, nil
	}

	ideal := total / len(r.Shards)
	threshold := r.Config.Threshold
	if threshold == 0 {
		threshold = 20
	}

	maxAllowed := ideal + (ideal * threshold / 100)

	var overloaded []string
	var underloaded []string
	for _, shard := range r.Shards {
		if counts[shard] > maxAllowed {
			overloaded = append(overloaded, shard)
		} else if counts[shard] < ideal {
			underloaded = append(underloaded, shard)
		}
	}

	if len(overloaded) == 0 || len(underloaded) == 0 {
		return 0, nil
	}

	toMove := 0
	for _, shard := range overloaded {
		toMove += counts[shard] - ideal
	}

	movesPercent := r.Config.MovesPerCycle
	if movesPercent == 0 {
		movesPercent = 10
	}
	minMoves := r.Config.MinMovesPerCycle
	if minMoves == 0 {
		minMoves = 10
	}

	maxMoves := toMove * movesPercent / 100
	if maxMoves < minMoves {
		maxMoves = minMoves
	}
	if maxMoves > toMove {
		maxMoves = toMove
	}

	limiter := rate.NewLimiter(rate.Limit(r.rateLimit()), 1)
	moved := 0

	for _, shard := range overloaded {
		if moved >= maxMoves {
			break
		}

		excess := counts[shard] - ideal
		if excess <= 0 {
			continue
		}

		selector, err := labels.Parse(r.LabelKey + "=" + shard)
		if err != nil {
			return moved, err
		}

		list := &metav1.PartialObjectMetadataList{}
		list.SetGroupVersionKind(r.GVK)

		limit := maxMoves - moved
		if limit > excess {
			limit = excess
		}

		if err := r.Client.List(ctx, list,
			client.MatchingLabelsSelector{Selector: selector},
			client.Limit(int64(limit)),
		); err != nil {
			return moved, err
		}

		targetIdx := 0
		for i := range list.Items {
			if moved >= maxMoves {
				break
			}

			if err := limiter.Wait(ctx); err != nil {
				return moved, err
			}

			for targetIdx < len(underloaded) && counts[underloaded[targetIdx]] >= ideal {
				targetIdx++
			}
			if targetIdx >= len(underloaded) {
				break
			}

			item := &list.Items[i]
			targetShard := underloaded[targetIdx]

			patch := client.MergeFrom(item.DeepCopy())
			item.Labels[r.LabelKey] = targetShard
			if err := r.Client.Patch(ctx, item, patch); err != nil {
				return moved, fmt.Errorf("moving resource to shard %s: %w", targetShard, err)
			}

			counts[shard]--
			counts[targetShard]++
			moved++
		}
	}

	return moved, nil
}

func (r *Rebalancer) rateLimit() int {
	if r.Config.RateLimit > 0 {
		return r.Config.RateLimit
	}
	return 10
}
