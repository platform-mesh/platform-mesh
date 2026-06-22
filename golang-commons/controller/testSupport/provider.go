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

package testSupport

import (
	"context"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

type FakeProvider struct {
	cfg *rest.Config
}

func NewFakeProvider(cfg *rest.Config) *FakeProvider {
	return &FakeProvider{cfg: cfg}
}

func (f FakeProvider) Get(context.Context, multicluster.ClusterName) (cluster.Cluster, error) {
	return cluster.New(f.cfg, nil)
}

// IndexField indexes the given object by the given field on all engaged
// clusters, current and future.
func (f FakeProvider) IndexField(context.Context, client.Object, string, client.IndexerFunc) error {
	return nil
}
