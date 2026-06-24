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

package reconciler

import (
	"strings"

	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// ClusterName strips the multi-provider prefix from a cluster name.
// The multi.Provider from multicluster-runtime prefixes cluster names as
// "providerName#clusterName". This function returns the original cluster name.
func ClusterName(name multicluster.ClusterName) string {
	if _, after, ok := strings.Cut(string(name), "#"); ok {
		return after
	}
	return string(name)
}
