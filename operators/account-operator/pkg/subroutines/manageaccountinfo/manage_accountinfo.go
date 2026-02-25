package manageaccountinfo

import (
	"context"
	"fmt"
	"strings"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/platform-mesh/golang-commons/logger"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/clusteredname"
)

var _ subroutine.Subroutine = (*ManageAccountInfoSubroutine)(nil)

const (
	ManageAccountInfoSubroutineName = "ManageAccountInfoSubroutine"
	DefaultAccountInfoName          = "account"
)

type ManageAccountInfoSubroutine struct {
	mgr      mcmanager.Manager
	serverCA string
	limiter  workqueue.TypedRateLimiter[*v1alpha1.Account]
}

func New(mgr mcmanager.Manager, serverCA string) *ManageAccountInfoSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[*v1alpha1.Account](1*time.Second, 120*time.Second)
	return &ManageAccountInfoSubroutine{mgr: mgr, serverCA: serverCA, limiter: exp}
}

func (r *ManageAccountInfoSubroutine) GetName() string {
	return ManageAccountInfoSubroutineName
}

func (r *ManageAccountInfoSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string { // coverage-ignore
	return []string{}
}

func (r *ManageAccountInfoSubroutine) Finalize(_ context.Context, _ runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

func (r *ManageAccountInfoSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)

	log := logger.LoadLoggerFromContext(ctx)
	cn := clusteredname.MustGetClusteredName(ctx, ro)

	clusterRef, err := r.mgr.GetCluster(ctx, string(cn.ClusterID))
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting cluster: %w", err), true, true)
	}
	clusterClient := clusterRef.GetClient()

	accountWorkspace := &kcptenancyv1alpha.Workspace{}
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: instance.Name}, accountWorkspace); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting Account's Workspace: %w", err), true, true)
	}

	if accountWorkspace.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseInitializing && accountWorkspace.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseReady {
		log.Info().Msg("workspace is not ready yet, retry")
		return ctrl.Result{RequeueAfter: r.limiter.When(instance)}, nil
	}

	// Retrieve logical cluster
	currentWorkspacePath, currentWorkspaceUrl, err := r.retrieveCurrentWorkspacePath(accountWorkspace)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	selfAccountLocation := v1alpha1.AccountLocation{
		Name:               instance.Name,
		GeneratedClusterId: accountWorkspace.Spec.Cluster,
		OriginClusterId:    string(cn.ClusterID),
		Type:               instance.Spec.Type,
		Path:               currentWorkspacePath,
		URL:                currentWorkspaceUrl,
	}

	accountCluster, err := r.mgr.GetCluster(ctx, accountWorkspace.Spec.Cluster)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	accountClusterClient := accountCluster.GetClient()

	// Create AccountInfo for an organization
	if instance.Spec.Type == v1alpha1.AccountTypeOrg {
		accountInfo := &v1alpha1.AccountInfo{ObjectMeta: ctrl.ObjectMeta{Name: DefaultAccountInfoName}}
		if _, err := controllerutil.CreateOrPatch(ctx, accountClusterClient, accountInfo, func() error {
			// the .Spec.FGA.Store.ID is set from an external workspace initializer
			accountInfo.Spec.Account = selfAccountLocation
			accountInfo.Spec.ParentAccount = nil
			accountInfo.Spec.Organization = selfAccountLocation
			accountInfo.Spec.ClusterInfo.CA = r.serverCA
			return nil
		}); err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		r.limiter.Forget(instance)
		return ctrl.Result{}, nil
	}

	// Create AccountInfo for a non-organization Account based on its parent's
	// AccountInfo
	var parentAccountInfo v1alpha1.AccountInfo
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: DefaultAccountInfoName}, &parentAccountInfo); kerrors.IsNotFound(err) {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("AccountInfo does not yet exist"), true, false)
	} else if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting parent AccountInfo: %w", err), true, true)
	}

	accountInfo := &v1alpha1.AccountInfo{ObjectMeta: v1.ObjectMeta{Name: DefaultAccountInfoName}}
	if _, err := controllerutil.CreateOrUpdate(ctx, accountClusterClient, accountInfo, func() error {
		accountInfo.Spec.Account = selfAccountLocation
		accountInfo.Spec.ParentAccount = &parentAccountInfo.Spec.Account
		accountInfo.Spec.Organization = parentAccountInfo.Spec.Organization
		accountInfo.Spec.FGA.Store.Id = parentAccountInfo.Spec.FGA.Store.Id
		accountInfo.Spec.OIDC = parentAccountInfo.Spec.OIDC
		accountInfo.Spec.ClusterInfo.CA = r.serverCA
		return nil
	}); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("creating or updating AccountInfo %w", err), true, true)
	}

	r.limiter.Forget(instance)
	return ctrl.Result{}, nil
}

func (r *ManageAccountInfoSubroutine) retrieveCurrentWorkspacePath(ws *kcptenancyv1alpha.Workspace) (string, string, error) {
	if ws.Spec.URL == "" {
		return "", "", fmt.Errorf("workspace URL is empty")
	}

	// Parse path from URL
	split := strings.Split(ws.Spec.URL, "/")
	if len(split) < 3 {
		return "", "", fmt.Errorf("workspace URL is invalid")
	}

	lastSegment := split[len(split)-1]
	if lastSegment == "" || strings.TrimSpace(lastSegment) == "" {
		return "", "", fmt.Errorf("workspace URL is empty")
	}
	return lastSegment, ws.Spec.URL, nil
}
