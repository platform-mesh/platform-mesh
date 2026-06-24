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

package subroutine

import (
	"context"
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

const (
	AccountInfoFinalizer = "security.platform-mesh.io/accountinfo-finalizer"
	APIBindingFinalizer  = "core.platform-mesh.io/apibinding-finalizer"
)

type AccountInfoFinalizerSubroutine struct {
	mgr mcmanager.Manager
}

func NewAccountInfoFinalizerSubroutine(mgr mcmanager.Manager) *AccountInfoFinalizerSubroutine {
	return &AccountInfoFinalizerSubroutine{
		mgr: mgr,
	}
}

var _ subroutines.Subroutine = &AccountInfoFinalizerSubroutine{}

func (a *AccountInfoFinalizerSubroutine) GetName() string {
	return "AccountInfoFinalizer"
}

func (a *AccountInfoFinalizerSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{AccountInfoFinalizer}
}

func (a *AccountInfoFinalizerSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)
	_ = obj.(*pmcorev1alpha1.AccountInfo)

	cluster, err := a.mgr.ClusterFromContext(ctx)
	if err != nil {
		return subroutines.OK(), err
	}

	var apiBindings kcpapisv1alpha2.APIBindingList
	if err := cluster.GetClient().List(ctx, &apiBindings); err != nil {
		return subroutines.OK(), err
	}

	for _, binding := range apiBindings.Items {
		if controllerutil.ContainsFinalizer(&binding, APIBindingFinalizer) {
			log.Debug().
				Str("apibinding", binding.Name).
				Msg("APIBinding still has finalizer, requeuing AccountInfo deletion")
			return subroutines.StopWithRequeue(5*time.Second, "APIBinding still has finalizer, requeuing AccountInfo deletion"), nil
		}
	}

	log.Info().Msg("No APIBindings with finalizer found, allowing AccountInfo deletion")
	return subroutines.OK(), nil
}
