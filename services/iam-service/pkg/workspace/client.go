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

package workspace

import (
	"context"
	"fmt"

	"go.platform-mesh.io/golang-commons/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// ClientFactory creates a client for a specific KCP workspace
type ClientFactory interface {
	New(ctx context.Context, accountPath multicluster.ClusterName) (client.Client, error)
}

// KCPClient implements ClientFactory for KCP workspaces
type KCPClient struct {
	mgr mcmanager.Manager
}

// NewClientFactory creates a new workspace client factory
func NewClientFactory(mgr mcmanager.Manager) *KCPClient {
	return &KCPClient{
		mgr: mgr,
	}
}

// New creates a new client for the specified workspace path
func (f *KCPClient) New(ctx context.Context, accountPath multicluster.ClusterName) (client.Client, error) {
	log := logger.LoadLoggerFromContext(ctx)
	cluster, err := f.mgr.GetCluster(ctx, accountPath)
	if err != nil {
		log.Err(err).Msg(fmt.Sprintf("failed to get cluster: %s", accountPath))
		return nil, err
	}

	return cluster.GetClient(), nil
}
