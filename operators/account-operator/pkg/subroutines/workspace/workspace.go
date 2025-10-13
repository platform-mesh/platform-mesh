package workspace

import (
	"context"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	conditionsapi "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	conditionshelper "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/util/conditions"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/clusteredname"
	"github.com/platform-mesh/account-operator/pkg/subroutines/accountinfo"
	"github.com/platform-mesh/account-operator/pkg/subroutines/util"
)

const (
	WorkspaceSubroutineName      = "WorkspaceSubroutine"
	WorkspaceSubroutineFinalizer = "account.core.platform-mesh.io/finalizer"
	orgsWorkspacePath            = "root:orgs"
)

type WorkspaceSubroutine struct {
	mgr        mcmanager.Manager
	limiter    workqueue.TypedRateLimiter[clusteredname.ClusteredName]
	orgsClient client.Client
}

func New(mgr mcmanager.Manager, orgsClient client.Client) *WorkspaceSubroutine {
	return &WorkspaceSubroutine{
		mgr:        mgr,
		limiter:    workqueue.NewTypedItemExponentialFailureRateLimiter[clusteredname.ClusteredName](1*time.Second, 120*time.Second),
		orgsClient: orgsClient,
	}
}

func (r *WorkspaceSubroutine) GetName() string {
	return WorkspaceSubroutineName
}

func (r *WorkspaceSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*corev1alpha1.Account)
	cn := clusteredname.MustGetClusteredName(ctx, ro)

	clusterName := cn.ClusterID.String()

	cluster, err := r.mgr.GetCluster(ctx, clusterName)
	if err != nil { // coverage-ignore
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	clusterClient := cluster.GetClient()

	ws := kcptenancyv1alpha.Workspace{}
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: instance.Name}, &ws); err != nil {
		if kerrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if ws.GetDeletionTimestamp() != nil {
		return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
	}

	if err := clusterClient.Delete(ctx, &ws); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
}

func (r *WorkspaceSubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string { // coverage-ignore
	return []string{WorkspaceSubroutineFinalizer}
}

func (r *WorkspaceSubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*corev1alpha1.Account)
	cn := clusteredname.MustGetClusteredName(ctx, ro)

	clusterName := cn.ClusterID.String()

	clusterRef, err := r.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	clusterClient := clusterRef.GetClient()

	workspaceTypeName := util.GetOrgWorkspaceTypeName(instance.Name)
	if instance.Spec.Type == corev1alpha1.AccountTypeAccount {

		accountInfo := &corev1alpha1.AccountInfo{}
		if err := clusterClient.Get(ctx, client.ObjectKey{Name: accountinfo.DefaultAccountInfoName}, accountInfo); err != nil {
			if kerrors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
			}

			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}

		if accountInfo.Spec.Organization.Name == "" {
			return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
		}

		workspaceTypeName = util.GetAccountWorkspaceTypeName(accountInfo.Spec.Organization.Name)
	}

	ready, err := r.checkWorkspaceTypeReady(ctx, workspaceTypeName)
	if err != nil { // coverage-ignore
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	if !ready { // coverage-ignore
		return ctrl.Result{RequeueAfter: r.limiter.When(cn)}, nil
	}

	createdWorkspace := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: instance.Name}}
	_, err = controllerutil.CreateOrUpdate(ctx, clusterClient, createdWorkspace, func() error {
		createdWorkspace.Spec.Type = &kcptenancyv1alpha.WorkspaceTypeReference{
			Name: kcptenancyv1alpha.WorkspaceTypeName(workspaceTypeName),
			Path: orgsWorkspacePath,
		}

		return controllerutil.SetOwnerReference(instance, createdWorkspace, clusterClient.Scheme())
	})
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	r.limiter.Forget(cn)
	return ctrl.Result{}, nil
}

// TODO: could potentially work without the orgsClient when we look up the orgs workspaceid on startup
func (r *WorkspaceSubroutine) checkWorkspaceTypeReady(ctx context.Context, workspaceTypeName string) (bool, error) {
	wst := &kcptenancyv1alpha.WorkspaceType{}
	if err := r.orgsClient.Get(ctx, client.ObjectKey{Name: workspaceTypeName}, wst); err != nil { // coverage-ignore
		if kerrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}
	return conditionshelper.IsTrue(wst, conditionsapi.ReadyCondition), nil
}
