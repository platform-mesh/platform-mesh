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
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// WithClustersFromNamedProvider engages only clusters from one named sub-provider, matched by name prefix since mcbuilder.WithClustersFromProvider always fails here.
func WithClustersFromNamedProvider(providerName, separator string) mcbuilder.EngageOptions {
	prefix := providerName + separator
	return mcbuilder.WithClusterFilter(func(clusterName multicluster.ClusterName, _ cluster.Cluster) bool {
		return strings.HasPrefix(clusterName.String(), prefix)
	})
}
