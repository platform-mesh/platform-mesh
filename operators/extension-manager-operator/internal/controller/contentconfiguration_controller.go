/*
Copyright 2024.

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
	"net/http"

	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/internal/config"
	extsub "go.platform-mesh.io/extension-manager-operator/pkg/subroutines"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation"
)

var contentConfigurationReconcilerName = "ContentConfigurationReconcilerCR"

// ContentConfigurationReconciler reconciles a ContentConfiguration object
type ContentConfigurationReconciler struct {
	lifecycle *lifecycle.Lifecycle
}

func NewContentConfigurationReconciler(log *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig) *ContentConfigurationReconciler {
	var subs []subroutines.Subroutine
	if cfg.SubroutinesContentConfigurationEnabled {
		subs = append(subs, extsub.NewContentConfigurationSubroutine(validation.NewContentConfiguration(), http.DefaultClient))
	}
	lc := lifecycle.New(mgr, contentConfigurationReconcilerName, func() client.Object {
		return &v1alpha1.ContentConfiguration{}
	}, subs...).
		WithConditions(conditions.NewManager()).
		WithSpread(contentConfigurationSpreadManager{}).
		WithPrepareContext(func(ctx context.Context, obj client.Object) (context.Context, error) {
			return logger.SetLoggerInContext(ctx, log.ComponentLogger("ContentConfiguration")), nil
		})
	return &ContentConfigurationReconciler{lifecycle: lc}
}

func (r *ContentConfigurationReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}

func (r *ContentConfigurationReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformmeshconfig.CommonServiceConfig, log *logger.Logger, eventPredicates ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, eventPredicates...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named(contentConfigurationReconcilerName).
		For(&v1alpha1.ContentConfiguration{}).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}
