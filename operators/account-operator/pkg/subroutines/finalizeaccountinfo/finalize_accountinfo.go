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

package finalizeaccountinfo

import (
	"context"
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/util/workqueue"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

var _ subroutines.Finalizer = (*FinalizeAccountInfoSubroutine)(nil)

const (
	FinalizeAccountInfoSubroutineName = "FinalizeAccountInfoSubroutine"
	AccountInfoFinalizer              = "account.core.platform-mesh.io/info"
)

type FinalizeAccountInfoSubroutine struct {
	mgr     mcmanager.Manager
	limiter workqueue.TypedRateLimiter[*pmcorev1alpha1.AccountInfo]
}

func New(mgr mcmanager.Manager) (*FinalizeAccountInfoSubroutine, error) {
	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[*pmcorev1alpha1.AccountInfo](
		ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}
	return &FinalizeAccountInfoSubroutine{mgr: mgr, limiter: rl}, nil
}

func (r *FinalizeAccountInfoSubroutine) GetName() string {
	return FinalizeAccountInfoSubroutineName
}

func (r *FinalizeAccountInfoSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{AccountInfoFinalizer}
}

func (r *FinalizeAccountInfoSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	instance := obj.(*pmcorev1alpha1.AccountInfo)
	log := logger.LoadLoggerFromContext(ctx)

	cluster, err := r.mgr.ClusterFromContext(ctx)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting cluster from context: %w", err)
	}
	clusterClient := cluster.GetClient()

	list := &pmcorev1alpha1.AccountList{}
	if err := clusterClient.List(ctx, list, &ctrlruntimeclient.ListOptions{}); err != nil {
		if !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
			return subroutines.OK(), fmt.Errorf("listing child accounts: %w", err)
		}
	}

	if len(list.Items) > 0 {
		log.Info().Msgf("Found %d accounts, cannot finalize AccountInfo yet", len(list.Items))
		return subroutines.StopWithRequeue(r.limiter.When(instance), "Accounts still exist"), nil
	}

	log.Info().Msg("No accounts found in cluster, AccountInfo can be finalized")

	r.limiter.Forget(instance)
	return subroutines.OK(), nil
}
