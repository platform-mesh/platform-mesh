package subroutines

import (
	"context"
	"fmt"
	"strings"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
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
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
)

var _ subroutine.Subroutine = (*AccountInfoSubroutine)(nil)

const (
	AccountInfoSubroutineName = "AccountInfoSubroutine"
	DefaultAccountInfoName    = "account"
)

type AccountInfoSubroutine struct {
	client   client.Client
	serverCA string
	limiter  workqueue.TypedRateLimiter[ClusteredName]
}

func NewAccountInfoSubroutine(client client.Client, serverCA string) *AccountInfoSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Second, 120*time.Second)
	return &AccountInfoSubroutine{client: client, serverCA: serverCA, limiter: exp}
}

func (r *AccountInfoSubroutine) GetName() string {
	return AccountInfoSubroutineName
}

func (r *AccountInfoSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	cn := MustGetClusteredName(ctx, ro)

	// The account info object is relevant input for other finalizers, removing the accountinfo finalizer at last
	if len(ro.GetFinalizers()) > 1 {
		delay := r.limiter.When(cn)
		return ctrl.Result{RequeueAfter: delay}, nil
	}

	r.limiter.Forget(cn)
	return ctrl.Result{}, nil
}

func (r *AccountInfoSubroutine) Finalizers() []string { // coverage-ignore
	return []string{"account.core.platform-mesh.io/info"}
}

func (r *AccountInfoSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)
	log := logger.LoadLoggerFromContext(ctx)
	cn := MustGetClusteredName(ctx, ro)

	// select workspace for account
	accountWorkspace, err := retrieveWorkspace(ctx, instance, r.client, log)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if accountWorkspace.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseReady {
		log.Info().Msg("workspace is not ready yet, retry")
		delay := r.limiter.When(cn)
		return ctrl.Result{RequeueAfter: delay}, nil
	}

	// Prepare context to work in workspace
	wsCtx := kontext.WithCluster(ctx, logicalcluster.Name(accountWorkspace.Spec.Cluster))

	// Retrieve logical cluster
	currentWorkspacePath, currentWorkspaceUrl, err := r.retrieveCurrentWorkspacePath(accountWorkspace)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	originCluster, ok := instance.GetAnnotations()["kcp.io/cluster"]
	if !ok {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("origin cluster not found"), true, false)
	}
	selfAccountLocation := v1alpha1.AccountLocation{
		Name:               instance.Name,
		GeneratedClusterId: accountWorkspace.Spec.Cluster,
		OriginClusterId:    originCluster,
		Type:               instance.Spec.Type,
		Path:               currentWorkspacePath,
		URL:                currentWorkspaceUrl,
	}

	if instance.Spec.Type == v1alpha1.AccountTypeOrg {
		accountInfo := &v1alpha1.AccountInfo{ObjectMeta: v1.ObjectMeta{Name: DefaultAccountInfoName}}
		_, err = controllerutil.CreateOrPatch(wsCtx, r.client, accountInfo, func() error {
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

	parentAccountInfo, exists, err := r.retrieveAccountInfo(ctx, log)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if !exists {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("AccountInfo does not yet exist. Retry another time"), true, false)
	}

	accountInfo := &v1alpha1.AccountInfo{ObjectMeta: v1.ObjectMeta{Name: DefaultAccountInfoName}}
	_, err = controllerutil.CreateOrUpdate(wsCtx, r.client, accountInfo, func() error {
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

func (r *AccountInfoSubroutine) retrieveAccountInfo(ctx context.Context, log *logger.Logger) (*v1alpha1.AccountInfo, bool, error) {
	accountInfo := &v1alpha1.AccountInfo{}
	err := r.client.Get(ctx, client.ObjectKey{Name: "account"}, accountInfo)
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
