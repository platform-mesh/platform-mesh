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

package controller

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/builder"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/logger"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"go.platform-mesh.io/search-operator/internal/subroutine"
)

// APIBindingReconciler watches APIBinding resources across all workspaces
type APIBindingReconciler struct {
	log         *logger.Logger
	mclifecycle *multicluster.LifecycleManager
}

// NewAPIBindingReconciler creates a new APIBinding reconciler.
func NewAPIBindingReconciler(log *logger.Logger, mcMgr mcmanager.Manager, indexPrefix string) (*APIBindingReconciler, error) {
	localMgr := mcMgr.GetLocalManager()

	orgsClient, err := subroutine.GetScopedClient(localMgr.GetConfig(), localMgr.GetScheme(), "root:orgs")
	if err != nil {
		return nil, fmt.Errorf("create root:orgs scoped client: %w", err)
	}

	watcherSubroutine, err := subroutine.NewAPIBindingWatcherSubroutine(mcMgr, orgsClient, localMgr.GetConfig(), indexPrefix)
	if err != nil {
		return nil, fmt.Errorf("create APIBindingWatcherSubroutine: %w", err)
	}

	return &APIBindingReconciler{
		log: log,
		mclifecycle: builder.NewBuilder("apibinding", "APIBindingReconciler", []lifecyclesubroutine.Subroutine{
			watcherSubroutine,
		}, log).BuildMultiCluster(mcMgr),
	}, nil
}

// +kubebuilder:rbac:groups=apis.kcp.io,resources=apibindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=apis.kcp.io,resources=apiexports,verbs=get;list;watch
// +kubebuilder:rbac:groups=apis.kcp.io,resources=apiresourceschemas,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.platform-mesh.io,resources=accountinfos,verbs=get;list;watch

// Reconcile handles APIBinding reconciliation
func (r *APIBindingReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctxWithCluster := mccontext.WithCluster(ctx, req.ClusterName)
	return r.mclifecycle.Reconcile(ctxWithCluster, req, &kcpv1alpha1.APIBinding{})
}

// SetupWithManager sets up the controller with the multicluster Manager.
func (r *APIBindingReconciler) SetupWithManager(mgr mcmanager.Manager, maxConcurrentReconciles int, evp ...predicate.Predicate) error {
	return r.mclifecycle.SetupWithManager(mgr, maxConcurrentReconciles, "apibinding", &kcpv1alpha1.APIBinding{}, "", r, r.log, evp...)
}

// GetAllClient creates a client that can query across all workspaces using the wildcard cluster
func GetAllClient(config *rest.Config, scheme *runtime.Scheme) (client.Client, error) {
	allCfg := rest.CopyConfig(config)

	parsed, err := url.Parse(allCfg.Host)
	if err != nil {
		return nil, err
	}

	// Extract the base path before "clusters" and append wildcard
	parts := strings.Split(parsed.Path, "clusters")
	if len(parts) > 0 {
		parsed.Path, err = url.JoinPath(parts[0], "clusters", logicalcluster.Wildcard.String())
		if err != nil {
			return nil, err
		}
	} else {
		parsed.Path, err = url.JoinPath("/", "clusters", logicalcluster.Wildcard.String())
		if err != nil {
			return nil, err
		}
	}

	allCfg.Host = parsed.String()

	return client.New(allCfg, client.Options{Scheme: scheme})
}
