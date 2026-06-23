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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TargetResource struct {
	// +kubebuilder:validation:Required
	Group string `json:"group"`
	// +kubebuilder:validation:Required
	Version string `json:"version"`
	// +kubebuilder:validation:Required
	Resource string `json:"resource"`
}

type ShardRef struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type RebalanceConfig struct {
	// +kubebuilder:default="5m"
	// +kubebuilder:validation:XValidation:rule="self != \"0s\"",message="interval must be greater than zero"
	Interval metav1.Duration `json:"interval,omitempty"`
	// +kubebuilder:default=20
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Threshold int `json:"threshold,omitempty"`
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	MovesPerCycle int `json:"movesPerCycle,omitempty"`
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	MinMovesPerCycle int `json:"minMovesPerCycle,omitempty"`
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	RateLimit int `json:"rateLimit,omitempty"`
}

type WebhookConfig struct {
	Enabled bool `json:"enabled,omitempty"`
}

type ResourceShardingSpec struct {
	// +kubebuilder:validation:Required
	Target TargetResource `json:"target"`
	// +kubebuilder:default="sharding.platform-mesh.io/shard"
	ShardLabelKey string `json:"shardLabelKey,omitempty"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Shards    []ShardRef      `json:"shards"`
	Rebalance RebalanceConfig `json:"rebalance,omitempty"`
	Webhook   WebhookConfig   `json:"webhook,omitempty"`
}

type ShardDistribution struct {
	Shard string `json:"shard"`
	Count int    `json:"count"`
}

type ResourceShardingStatus struct {
	Distribution       []ShardDistribution `json:"distribution,omitempty"`
	TotalShards        int                 `json:"totalShards,omitempty"`
	LastRebalanceTime  *metav1.Time        `json:"lastRebalanceTime,omitempty"`
	Conditions         []metav1.Condition  `json:"conditions,omitempty"`
	ObservedGeneration int64               `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.target.resource`
// +kubebuilder:printcolumn:name="Shards",type=integer,JSONPath=`.status.totalShards`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// ResourceSharding defines the sharding configuration for a target resource type.
type ResourceSharding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceShardingSpec   `json:"spec,omitempty"`
	Status ResourceShardingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceShardingList contains a list of ResourceSharding
type ResourceShardingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceSharding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourceSharding{}, &ResourceShardingList{})
}
