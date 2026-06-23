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

import "sync/atomic"

type ShardAssigner struct {
	shards  atomic.Pointer[[]string]
	counter atomic.Uint64
}

func NewShardAssigner(shards []string) *ShardAssigner {
	a := &ShardAssigner{}
	a.shards.Store(&shards)
	return a
}

// Next returns the next shard in round-robin order.
// Returns "" if no shards are configured.
func (a *ShardAssigner) Next() string {
	shards := *a.shards.Load()
	if len(shards) == 0 {
		return ""
	}
	idx := a.counter.Add(1) - 1
	return shards[idx%uint64(len(shards))]
}

// UpdateShards atomically replaces the shard list.
func (a *ShardAssigner) UpdateShards(shards []string) {
	a.shards.Store(&shards)
}
