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

package lifecycle

import (
	"context"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

// fakeManager implements mcmanager.Manager for tests by embedding the interface
// and only overriding ClusterFromContext.
type fakeManager struct {
	mcmanager.Manager
	cl ctrlruntimeclient.Client
}

func (f *fakeManager) ClusterFromContext(context.Context) (cluster.Cluster, error) {
	return &fakeCluster{cl: f.cl}, nil
}

type fakeCluster struct {
	cluster.Cluster
	cl ctrlruntimeclient.Client
}

func (f *fakeCluster) GetClient() ctrlruntimeclient.Client { return f.cl }

type fakeManagerWithError struct {
	mcmanager.Manager
	err error
}

func (f *fakeManagerWithError) ClusterFromContext(context.Context) (cluster.Cluster, error) {
	return nil, f.err
}
