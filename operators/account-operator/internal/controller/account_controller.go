package controller

import (
	"context"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/builder"
	mclifecycle "github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"

	"github.com/platform-mesh/golang-commons/logger"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines"
	"github.com/platform-mesh/account-operator/pkg/subroutines/manageaccountinfo"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspace"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspacetype"
)

const (
	operatorName          = "account-operator"
	accountReconcilerName = "AccountReconciler"
)

// AccountReconciler orchestrates Account resources across logical clusters.
type AccountReconciler struct {
	cfg       config.OperatorConfig
	lifecycle *mclifecycle.LifecycleManager
}

func NewAccountReconciler(log *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig, orgsClient client.Client, fgaClient openfgav1.OpenFGAServiceClient) *AccountReconciler { // coverage-ignore
	localMgr := mgr.GetLocalManager()
	localCfg := rest.CopyConfig(localMgr.GetConfig())
	serverCA := string(localCfg.CAData)

	subs := []lifecyclesubroutine.Subroutine{}

	if cfg.Subroutines.WorkspaceType.Enabled {
		subs = append(subs, workspacetype.New(orgsClient))
	}

	if cfg.Subroutines.Workspace.Enabled {
		subs = append(subs, workspace.New(mgr, orgsClient))
	}

	if cfg.Subroutines.AccountInfo.Enabled {
		subs = append(subs, manageaccountinfo.New(mgr, serverCA))
	}

	if cfg.Subroutines.FGA.Enabled {
		subs = append(subs, subroutines.NewFGASubroutine(mgr, fgaClient, cfg.Subroutines.FGA.CreatorRelation, cfg.Subroutines.FGA.ParentRelation, cfg.Subroutines.FGA.ObjectType))
	}

	return &AccountReconciler{
		cfg: cfg,
		lifecycle: builder.NewBuilder(operatorName, accountReconcilerName, subs, log).
			WithConditionManagement().
			BuildMultiCluster(mgr),
	}
}

func (r *AccountReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformmeshconfig.CommonServiceConfig, log *logger.Logger, eventPredicates ...predicate.Predicate) error { // coverage-ignore
	return r.lifecycle.SetupWithManager(mgr, cfg.MaxConcurrentReconciles, accountReconcilerName, &v1alpha1.Account{}, cfg.DebugLabelValue, r, log, eventPredicates...)
}

func (r *AccountReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) { // coverage-ignore
	return r.lifecycle.Reconcile(mccontext.WithCluster(ctx, req.ClusterName), req, &v1alpha1.Account{})
}
