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

	"go.platform-mesh.io/golang-commons/controller/lifecycle/builder"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/logger"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"go.platform-mesh.io/search-operator/internal/config"
	"go.platform-mesh.io/search-operator/internal/opensearch"
	"go.platform-mesh.io/search-operator/internal/subroutine"
)

type IndexableResourceReconciler struct {
	log         *logger.Logger
	mclifecycle *multicluster.LifecycleManager
	allClient   client.Client
	cfg         config.Config
	resource    unstructured.Unstructured
}

// NewIndexableResourceReconciler creates a new IndexableResource reconciler
// If osClient is nil, only the IndexableResourceWatcher subroutine is used (no indexing)
func NewIndexableResource(log *logger.Logger, cfg config.Config, mcMgr mcmanager.Manager, osClient *opensearch.Client, apiExportName string, resources *unstructured.Unstructured) (*IndexableResourceReconciler, error) {
	localMgr := mcMgr.GetLocalManager()

	// Create a wildcard client for cross-workspace queries
	allClient, err := GetAllClient(localMgr.GetConfig(), localMgr.GetScheme())
	if err != nil {
		return nil, err
	}

	// Create a client scoped to root:orgs for Workspace lookups
	orgsClient, err := subroutine.GetScopedClient(localMgr.GetConfig(), localMgr.GetScheme(), "root:orgs")
	if err != nil {
		return nil, err
	}

	// Build subroutines list
	watcherSubroutine, err := subroutine.NewIndexableResourceWatcherSubroutine(mcMgr, allClient, orgsClient, osClient, apiExportName, cfg.OpenSearch.IndexNamePrefix, localMgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("create IndexableResourceWatcherSubroutine: %w", err)
	}
	subroutines := []lifecyclesubroutine.Subroutine{watcherSubroutine}

	return &IndexableResourceReconciler{
		log:       log,
		allClient: allClient,
		mclifecycle: builder.NewBuilder("search-operator", "IndexableResourceReconciler", subroutines, log).
			BuildMultiCluster(mcMgr),
		cfg:      cfg,
		resource: *resources,
	}, nil
}

// +kubebuilder:rbac:groups=tenancy.kcp.io,resources=workspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.platform-mesh.io,resources=accountinfos,verbs=get;list;watch

// Reconcile handles IndexableResource reconciliation
func (r *IndexableResourceReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctxWithCluster := mccontext.WithCluster(ctx, req.ClusterName)
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(r.resource.GroupVersionKind())
	return r.mclifecycle.Reconcile(ctxWithCluster, req, obj)
}

// SetupWithManager sets up the controller with the multicluster Manager.
func (r *IndexableResourceReconciler) SetupWithManager(mgr mcmanager.Manager, maxConcurrentReconciles int, obj *unstructured.Unstructured, evp ...predicate.Predicate) error {
	return r.mclifecycle.SetupWithManager(mgr, maxConcurrentReconciles, fmt.Sprintf("indexableResourceReconciler-%s", obj.GetObjectKind().GroupVersionKind().String()), obj, "", r, r.log, evp...)
}
