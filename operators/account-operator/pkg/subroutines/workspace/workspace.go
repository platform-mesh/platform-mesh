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

	"go.platform-mesh.io/account-operator/pkg/clusteredname"
	"go.platform-mesh.io/account-operator/pkg/subroutines/manageaccountinfo"
	"go.platform-mesh.io/account-operator/pkg/subroutines/util"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	conditionsapi "github.com/kcp-dev/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	conditionshelper "github.com/kcp-dev/sdk/apis/third_party/conditions/util/conditions"
)

var _ subroutines.Processor = (*WorkspaceSubroutine)(nil)
var _ subroutines.Finalizer = (*WorkspaceSubroutine)(nil)

const (
	WorkspaceSubroutineName      = "WorkspaceSubroutine"
	WorkspaceSubroutineFinalizer = "account.core.platform-mesh.io/finalizer"
	orgsWorkspacePath            = "root:orgs"
)

type WorkspaceSubroutine struct {
	mgr     mcmanager.Manager
	limiter workqueue.TypedRateLimiter[*pmcorev1alpha1.Account]
}

func New(mgr mcmanager.Manager) (*WorkspaceSubroutine, error) {
	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[*pmcorev1alpha1.Account](
		ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}
	return &WorkspaceSubroutine{
		mgr:     mgr,
		limiter: rl,
	}, nil
}

func (r *WorkspaceSubroutine) GetName() string {
	return WorkspaceSubroutineName
}

func (r *WorkspaceSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{WorkspaceSubroutineFinalizer}
}

func (r *WorkspaceSubroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	instance := obj.(*pmcorev1alpha1.Account)
	cn := clusteredname.MustGetClusteredName(ctx, obj)

	clusterName := multicluster.ClusterName(cn.ClusterID.String())

	cluster, err := r.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return subroutines.OK(), err
	}

	clusterClient := cluster.GetClient()

	ws := kcptenancyv1alpha1.Workspace{}
	if err := clusterClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: instance.Name}, &ws); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			r.limiter.Forget(instance)
			return subroutines.OK(), nil
		}
		return subroutines.OK(), err
	}

	if ws.GetDeletionTimestamp() != nil {
		return subroutines.StopWithRequeue(r.limiter.When(instance), "Waiting for Workspace deletion"), nil
	}

	if err := clusterClient.Delete(ctx, &ws); err != nil {
		return subroutines.OK(), err
	}

	return subroutines.StopWithRequeue(r.limiter.When(instance), "Waiting for Workspace deletion"), nil
}

func (r *WorkspaceSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	instance := obj.(*pmcorev1alpha1.Account)
	cn := clusteredname.MustGetClusteredName(ctx, obj)

	clusterName := multicluster.ClusterName(cn.ClusterID.String())

	clusterRef, err := r.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return subroutines.OK(), err
	}
	clusterClient := clusterRef.GetClient()

	workspaceTypeName := util.GetWorkspaceTypeName(instance.Name, instance.Spec.Type)
	if instance.Spec.Type != pmcorev1alpha1.AccountTypeOrg {
		accountInfo := &pmcorev1alpha1.AccountInfo{}
		if err := clusterClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: manageaccountinfo.DefaultAccountInfoName}, accountInfo); err != nil {
			if apierrors.IsNotFound(err) {
				return subroutines.StopWithRequeue(r.limiter.When(instance), "AccountInfo not found yet"), nil
			}

			return subroutines.OK(), err
		}

		if accountInfo.Spec.Organization.Name == "" {
			return subroutines.StopWithRequeue(r.limiter.When(instance), "AccountInfo organization name not set yet"), nil
		}

		workspaceTypeName = util.GetWorkspaceTypeName(accountInfo.Spec.Organization.Name, instance.Spec.Type)
	}

	ready, err := r.checkWorkspaceTypeReady(ctx, workspaceTypeName)
	if err != nil {
		return subroutines.OK(), err
	}
	if !ready {
		return subroutines.StopWithRequeue(r.limiter.When(instance), "Workspace type not ready yet"), nil
	}

	createdWorkspace := &kcptenancyv1alpha1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: instance.Name}}
	if _, err = controllerutil.CreateOrUpdate(ctx, clusterClient, createdWorkspace, func() error {
		createdWorkspace.Spec.Type = &kcptenancyv1alpha1.WorkspaceTypeReference{
			Name: kcptenancyv1alpha1.WorkspaceTypeName(workspaceTypeName),
			Path: orgsWorkspacePath,
		}

		return controllerutil.SetOwnerReference(instance, createdWorkspace, clusterClient.Scheme())
	}); err != nil {
		return subroutines.OK(), err
	}

	r.limiter.Forget(instance)
	return subroutines.OK(), nil
}

// TODO: could potentially work without the orgsClient when we look up the
// orgs workspaceid on startup
func (r *WorkspaceSubroutine) checkWorkspaceTypeReady(ctx context.Context, workspaceTypeName string) (bool, error) {
	cluster, err := r.mgr.GetCluster(ctx, orgsWorkspacePath)
	if err != nil {
		return false, err
	}
	clusterClient := cluster.GetClient()

	log := logger.LoadLoggerFromContext(ctx)
	log.Info().Msg("Getting workspace using retrieved client")
	wst := &kcptenancyv1alpha1.WorkspaceType{}
	if err := clusterClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: workspaceTypeName}, wst); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}
	return conditionshelper.IsTrue(wst, conditionsapi.ReadyCondition), nil
}
