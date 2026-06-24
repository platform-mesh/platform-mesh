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
	"net/http"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

type FakeManager struct{ Client ctrlruntimeclient.Client }

func (f *FakeManager) GetCluster(context.Context, multicluster.ClusterName) (cluster.Cluster, error) {
	return &FakeCluster{client: f.Client}, nil
}

var _ cluster.Cluster = (*FakeCluster)(nil)

type FakeCluster struct{ client ctrlruntimeclient.Client }

func (f FakeCluster) GetHTTPClient() *http.Client {
	return nil
}

func (f FakeCluster) GetConfig() *rest.Config {
	return nil
}

func (f FakeCluster) GetCache() cache.Cache {
	return nil
}

func (f FakeCluster) GetScheme() *runtime.Scheme {
	return nil
}

func (f FakeCluster) GetClient() ctrlruntimeclient.Client {
	return f.client
}

func (f FakeCluster) GetFieldIndexer() ctrlruntimeclient.FieldIndexer {
	return nil
}

func (f FakeCluster) GetEventRecorderFor(string) record.EventRecorder {
	return nil
}

func (f FakeCluster) GetRESTMapper() meta.RESTMapper {
	return nil
}

func (f FakeCluster) GetAPIReader() ctrlruntimeclient.Reader {
	return nil
}

func (f FakeCluster) Start(context.Context) error {
	return nil
}

func (f FakeCluster) GetEventRecorder(string) events.EventRecorder {
	return nil
}
