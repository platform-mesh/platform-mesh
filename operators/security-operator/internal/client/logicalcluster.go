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

package client

import (
	"fmt"
	"net/url"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
)

// NewForLogicalCluster returns a client for a given logical cluster name or
// path, based on a KCP base config.
func NewForLogicalCluster(config *rest.Config, scheme *runtime.Scheme, clusterKey logicalcluster.Name) (ctrlruntimeclient.Client, error) {
	path := fmt.Sprintf("/clusters/%s", clusterKey)

	return clientForPath(config, scheme, path)
}

// clientForPath returns a client for a give raw URL path.
func clientForPath(config *rest.Config, scheme *runtime.Scheme, path string) (ctrlruntimeclient.Client, error) {
	copy := rest.CopyConfig(config)

	parsed, err := url.Parse(copy.Host)
	if err != nil {
		return nil, fmt.Errorf("parsing host from config: %w", err)
	}
	parsed.Path = path
	copy.Host = parsed.String()

	return ctrlruntimeclient.New(copy, ctrlruntimeclient.Options{
		Scheme: scheme,
	})
}
