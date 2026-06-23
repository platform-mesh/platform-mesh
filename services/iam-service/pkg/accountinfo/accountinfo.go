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

package accountinfo

import (
	"context"
	"fmt"
	"sync"

	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
	"go.platform-mesh.io/account-operator/pkg/subroutines/manageaccountinfo"
	accountsv1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

type Retriever interface {
	Get(ctx context.Context, accountPath multicluster.ClusterName) (*accountsv1alpha1.AccountInfo, error)
}

type accountInfoRetriever struct {
	mgr           mcmanager.Manager
	clusterClient kcpclientset.ClusterInterface
	clusterLocks  sync.Map
}

func New(mgr mcmanager.Manager, clusterClient kcpclientset.ClusterInterface) (Retriever, error) {
	if clusterClient == nil || mgr == nil {
		return nil, fmt.Errorf("cluster client and manager cannot be nil")
	}
	return &accountInfoRetriever{
		mgr:           mgr,
		clusterClient: clusterClient,
	}, nil
}

func (a *accountInfoRetriever) Get(ctx context.Context, accountPath multicluster.ClusterName) (*accountsv1alpha1.AccountInfo, error) {
	log := logger.LoadLoggerFromContext(ctx)

	//FIXME: This lock was necessary as we saw race conditions when processing multiple requests in parallel
	// The issue occurs when a cluster is requested for the first time and multiple requests are processed simultaneously
	// We will work with the KCP team to identify the root cause and remove this lock in future
	mu := a.getClusterLock(accountPath)
	mu.Lock()
	defer mu.Unlock()

	cc, err := a.mgr.GetCluster(ctx, accountPath)
	if err != nil { // coverage-ignore
		log.Error().Err(err).Msg("failed to get cluster from manager")
		return nil, err
	}

	cl := cc.GetClient()
	ai := &accountsv1alpha1.AccountInfo{}
	err = cl.Get(ctx, client.ObjectKey{Name: manageaccountinfo.DefaultAccountInfoName}, ai)
	if err != nil {
		log.Error().Err(err).Msg("failed to get orgs workspace from kcp")
		return nil, err
	}
	log.Debug().Msg("retrieved account info successfully")
	return ai, nil
}

func (a *accountInfoRetriever) getClusterLock(clusterName multicluster.ClusterName) *sync.Mutex {
	lock, _ := a.clusterLocks.LoadOrStore(clusterName, &sync.Mutex{})
	return lock.(*sync.Mutex)
}
