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

package storage

import (
	"context"

	"github.com/kcp-dev/logicalcluster/v3"
)

type clusterPathKey struct{}

func WithClusterPath(ctx context.Context, path logicalcluster.Path) context.Context {
	return context.WithValue(ctx, clusterPathKey{}, path)
}

func ClusterPathFrom(ctx context.Context) (logicalcluster.Path, bool) {
	path, ok := ctx.Value(clusterPathKey{}).(logicalcluster.Path)
	if !ok {
		return logicalcluster.Path{}, false
	}
	return path, true
}
