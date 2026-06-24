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

package manageaccountinfo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.platform-mesh.io/account-operator/internal/metrics"
	"go.platform-mesh.io/account-operator/pkg/clusteredname"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

var _ subroutines.Processor = (*ManageAccountInfoSubroutine)(nil)

const (
	ManageAccountInfoSubroutineName = "ManageAccountInfoSubroutine"
	DefaultAccountInfoName          = "account"
)

type ManageAccountInfoSubroutine struct {
	mgr      mcmanager.Manager
	serverCA string
	limiter  workqueue.TypedRateLimiter[*pmcorev1alpha1.Account]
}

func New(mgr mcmanager.Manager, serverCA string) (*ManageAccountInfoSubroutine, error) {
	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[*pmcorev1alpha1.Account](
		ratelimiter.NewConfig(
			ratelimiter.WithRequeueDelay(1*time.Second),
			ratelimiter.WithStaticWindow(1*time.Second),
			ratelimiter.WithExponentialInitialBackoff(1*time.Second),
			ratelimiter.WithExponentialMaxBackoff(120*time.Second),
		))
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}
	return &ManageAccountInfoSubroutine{mgr: mgr, serverCA: serverCA, limiter: rl}, nil
}

func (r *ManageAccountInfoSubroutine) GetName() string {
	return ManageAccountInfoSubroutineName
}

func (r *ManageAccountInfoSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	instance := obj.(*pmcorev1alpha1.Account)

	log := logger.LoadLoggerFromContext(ctx)
	cn := clusteredname.MustGetClusteredName(ctx, obj)

	clusterRef, err := r.mgr.GetCluster(ctx, multicluster.ClusterName(cn.ClusterID))
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting cluster: %w", err)
	}
	clusterClient := clusterRef.GetClient()

	accountWorkspace := &kcptenancyv1alpha1.Workspace{}
	if err := clusterClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: instance.Name}, accountWorkspace); err != nil {
		return subroutines.OK(), fmt.Errorf("getting Account's Workspace: %w", err)
	}

	if accountWorkspace.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseInitializing && accountWorkspace.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
		log.Info().Msg("workspace is not ready yet, retry")
		return subroutines.StopWithRequeue(r.limiter.When(instance), "Workspace not ready yet"), nil
	}

	// Retrieve logical cluster
	currentWorkspacePath, currentWorkspaceUrl, err := r.retrieveCurrentWorkspacePath(accountWorkspace)
	if err != nil {
		return subroutines.OK(), err
	}

	selfAccountLocation := pmcorev1alpha1.AccountLocation{
		Name:               instance.Name,
		GeneratedClusterId: accountWorkspace.Spec.Cluster,
		OriginClusterId:    string(cn.ClusterID),
		Type:               instance.Spec.Type,
		Path:               currentWorkspacePath,
		URL:                currentWorkspaceUrl,
	}

	accountCluster, err := r.mgr.GetCluster(ctx, multicluster.ClusterName(accountWorkspace.Spec.Cluster))
	if err != nil {
		return subroutines.OK(), err
	}
	accountClusterClient := accountCluster.GetClient()

	// Create AccountInfo for an organization
	if instance.Spec.Type == pmcorev1alpha1.AccountTypeOrg {
		accountInfo := &pmcorev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
		if _, err := controllerutil.CreateOrPatch(ctx, accountClusterClient, accountInfo, func() error {
			// the .Spec.FGA.Store.ID is set from an external workspace initializer
			accountInfo.Spec.Account = selfAccountLocation
			accountInfo.Spec.ParentAccount = nil
			accountInfo.Spec.Organization = selfAccountLocation
			accountInfo.Spec.ClusterInfo.CA = r.serverCA
			return nil
		}); err != nil {
			return subroutines.OK(), err
		}

		r.limiter.Forget(instance)
		duration := time.Since(instance.CreationTimestamp.Time).Seconds()
		metrics.OrgProvisioningDuration.WithLabelValues().Observe(duration)
		return subroutines.OK(), nil
	}

	// Create AccountInfo for a non-organization Account based on its parent's
	// AccountInfo
	var parentAccountInfo pmcorev1alpha1.AccountInfo
	if err := clusterClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: DefaultAccountInfoName}, &parentAccountInfo); err != nil {
		if apierrors.IsNotFound(err) {
			// todo(simontesar): is there really a situation where a parent AccountInfo does not exist YET?
			return subroutines.StopWithRequeue(r.limiter.When(instance), "Parent AccountInfo does not exist yet"), nil
		}
		return subroutines.OK(), fmt.Errorf("getting parent AccountInfo: %w", err)
	}

	accountInfo := &pmcorev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: DefaultAccountInfoName}}
	if _, err := controllerutil.CreateOrUpdate(ctx, accountClusterClient, accountInfo, func() error {
		accountInfo.Spec.Account = selfAccountLocation
		accountInfo.Spec.ParentAccount = &parentAccountInfo.Spec.Account
		accountInfo.Spec.Organization = parentAccountInfo.Spec.Organization
		accountInfo.Spec.FGA.Store.Id = parentAccountInfo.Spec.FGA.Store.Id
		accountInfo.Spec.OIDC = parentAccountInfo.Spec.OIDC
		accountInfo.Spec.ClusterInfo.CA = r.serverCA
		return nil
	}); err != nil {
		return subroutines.OK(), fmt.Errorf("creating or updating AccountInfo: %w", err)
	}

	r.limiter.Forget(instance)
	return subroutines.OK(), nil
}

func (r *ManageAccountInfoSubroutine) retrieveCurrentWorkspacePath(ws *kcptenancyv1alpha1.Workspace) (string, string, error) {
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
