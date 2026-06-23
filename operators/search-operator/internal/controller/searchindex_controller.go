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

	"go.platform-mesh.io/golang-commons/controller/lifecycle/builder"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"go.platform-mesh.io/apis/search/v1alpha1"
	"go.platform-mesh.io/search-operator/internal/opensearch"
	"go.platform-mesh.io/search-operator/internal/subroutine"
)

// SearchIndexReconciler reconciles a SearchIndex object
type SearchIndexReconciler struct {
	log         *logger.Logger
	mclifecycle *multicluster.LifecycleManager
}

// NewSearchIndexReconciler creates a new SearchIndex reconciler
func NewSearchIndexReconciler(
	log *logger.Logger,
	mcMgr mcmanager.Manager,
	osClient *opensearch.Client,
	staticIndexPrefix string,
	semanticModelID string,
) *SearchIndexReconciler {
	return &SearchIndexReconciler{
		log: log,
		mclifecycle: builder.NewBuilder("searchindex", "SearchIndexReconciler", []lifecyclesubroutine.Subroutine{
			subroutine.NewIndexLifecycleSubroutine(mcMgr, osClient, staticIndexPrefix, semanticModelID),
		}, log).BuildMultiCluster(mcMgr),
	}
}

// +kubebuilder:rbac:groups=search.platform-mesh.io,resources=searchindexes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=search.platform-mesh.io,resources=searchindexes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=search.platform-mesh.io,resources=searchindexes/finalizers,verbs=update
// +kubebuilder:rbac:groups=apis.kcp.io,resources=apibindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=apis.kcp.io,resources=apiexports,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.kcp.io,resources=logicalclusters,verbs=get;list;watch

// Reconcile handles SearchIndex reconciliation
func (r *SearchIndexReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctxWithCluster := mccontext.WithCluster(ctx, req.ClusterName)
	return r.mclifecycle.Reconcile(ctxWithCluster, req, &v1alpha1.SearchIndex{})
}

// SetupWithManager sets up the controller with the multicluster Manager.
func (r *SearchIndexReconciler) SetupWithManager(mgr mcmanager.Manager, maxConcurrentReconciles int, evp ...predicate.Predicate) error {
	return r.mclifecycle.SetupWithManager(mgr, maxConcurrentReconciles, "searchindex", &v1alpha1.SearchIndex{}, "", r, r.log, evp...)
}
