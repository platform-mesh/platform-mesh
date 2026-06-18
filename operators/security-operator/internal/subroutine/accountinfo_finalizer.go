package subroutine

import (
	"context"
	"time"

	accountv1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/platform-mesh/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (a *AccountInfoFinalizerSubroutine) Finalizers(_ client.Object) []string {
	return []string{AccountInfoFinalizer}
}

func (a *AccountInfoFinalizerSubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)
	_ = obj.(*accountv1alpha1.AccountInfo)

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
