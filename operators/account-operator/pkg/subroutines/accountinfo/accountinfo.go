package accountinfo

import (
	"context"
	"fmt"
	"strings"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/clusteredname"
)

var _ subroutine.Subroutine = (*AccountInfoSubroutine)(nil)

const (
	AccountInfoSubroutineName = "AccountInfoSubroutine"
	DefaultAccountInfoName    = "account"
	AccountInfoFinalizer      = "account.core.platform-mesh.io/info"
)

type AccountInfoSubroutine struct {
	mgr      mcmanager.Manager
	serverCA string
	limiter  workqueue.TypedRateLimiter[clusteredname.ClusteredName]
}

func New(mgr mcmanager.Manager, serverCA string) *AccountInfoSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[clusteredname.ClusteredName](1*time.Second, 120*time.Second)
	return &AccountInfoSubroutine{mgr: mgr, serverCA: serverCA, limiter: exp}
}

func (r *AccountInfoSubroutine) GetName() string {
	return AccountInfoSubroutineName
}

func (r *AccountInfoSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string { // coverage-ignore
	return []string{AccountInfoFinalizer}
}

func (r *AccountInfoSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	cn := clusteredname.MustGetClusteredName(ctx, ro)

	// The account info object is relevant input for other finalizers, removing the accountinfo finalizer at last
	if len(ro.GetFinalizers()) > 1 {
		return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
	}

	r.limiter.Forget(cn)
	return ctrl.Result{}, nil
}

func (r *AccountInfoSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)

	log := logger.LoadLoggerFromContext(ctx)
	cn := clusteredname.MustGetClusteredName(ctx, ro)

	clusterRef, err := r.mgr.GetCluster(ctx, string(cn.ClusterID))
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	clusterClient := clusterRef.GetClient()

	accountWorkspace := &kcptenancyv1alpha.Workspace{}
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: instance.Name}, accountWorkspace); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if accountWorkspace.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseReady {
		log.Info().Msg("workspace is not ready yet, retry")
		return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
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

	if instance.Spec.Type == v1alpha1.AccountTypeOrg {
		accountInfo := &v1alpha1.AccountInfo{ObjectMeta: v1.ObjectMeta{Name: DefaultAccountInfoName}}
		_, err = controllerutil.CreateOrPatch(ctx, accountClusterClient, accountInfo, func() error {
			// the .Spec.FGA.Store.ID is set from an external workspace initializer
			accountInfo.Spec.Account = selfAccountLocation
			accountInfo.Spec.ParentAccount = nil
			accountInfo.Spec.Organization = selfAccountLocation
			accountInfo.Spec.ClusterInfo.CA = r.serverCA
			return nil
		})
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		r.limiter.Forget(cn)
		return ctrl.Result{}, nil
	}

	parentAccountInfo, exists, err := r.retrieveAccountInfo(ctx, clusterClient, log)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if !exists {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("AccountInfo does not yet exist. Retry another time"), true, false)
	}

	accountInfo := &v1alpha1.AccountInfo{ObjectMeta: v1.ObjectMeta{Name: DefaultAccountInfoName}}
	_, err = controllerutil.CreateOrUpdate(ctx, accountClusterClient, accountInfo, func() error {
		accountInfo.Spec.Account = selfAccountLocation
		accountInfo.Spec.ParentAccount = &parentAccountInfo.Spec.Account
		accountInfo.Spec.Organization = parentAccountInfo.Spec.Organization
		accountInfo.Spec.FGA.Store.Id = parentAccountInfo.Spec.FGA.Store.Id
		accountInfo.Spec.ClusterInfo.CA = r.serverCA
		return nil
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	r.limiter.Forget(cn)
	return ctrl.Result{}, nil
}

func (r *AccountInfoSubroutine) retrieveAccountInfo(ctx context.Context, cl client.Client, log *logger.Logger) (*v1alpha1.AccountInfo, bool, error) {
	accountInfo := &v1alpha1.AccountInfo{}
	err := cl.Get(ctx, client.ObjectKey{Name: "account"}, accountInfo)
	if err != nil {
		if kerrors.IsNotFound(err) {
			log.Info().Msg("accountInfo does not yet exist, retry")
			return nil, false, nil
		}
		log.Error().Err(err).Msg("error retrieving accountInfo")
		return nil, false, err
	}
	return accountInfo, true, nil
}

func (r *AccountInfoSubroutine) retrieveCurrentWorkspacePath(ws *kcptenancyv1alpha.Workspace) (string, string, error) {
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
