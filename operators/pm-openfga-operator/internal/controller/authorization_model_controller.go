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
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/pm-openfga-operator/internal/metrics"
	"go.platform-mesh.io/pm-openfga-operator/internal/subroutine"
	"go.platform-mesh.io/subroutines/lifecycle"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

// AuthorizationModelReconciler reconciles the AuthorizationModels of one API group.
type AuthorizationModelReconciler struct {
	log       *logger.Logger
	lifecycle *lifecycle.Lifecycle
	ctx       context.Context
	kinds     Kinds
}

func NewAuthorizationModelReconciler(ctx context.Context, log *logger.Logger, fga openfgav1.OpenFGAServiceClient, mcMgr mcmanager.Manager, kinds Kinds) *AuthorizationModelReconciler {
	subs := chain(mcMgr, kinds.BeforeAuthorizationModel, kinds.WrapAuthorizationModel, subroutine.NewTupleSubroutine(fga, mcMgr))
	lc := lifecycle.New(mcMgr, "AuthorizationModelReconciler"+kinds.Suffix, kinds.NewAuthorizationModel, subs...)

	return &AuthorizationModelReconciler{
		log:       log,
		lifecycle: lc,
		ctx:       ctx,
		kinds:     kinds,
	}
}

func (r *AuthorizationModelReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	start := time.Now()
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues("authorizationmodel"+r.kinds.Suffix, labelResult).Inc()
	metrics.ReconcileDuration.WithLabelValues("authorizationmodel" + r.kinds.Suffix).Observe(time.Since(start).Seconds())
	return result, err
}

func (r *AuthorizationModelReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, evp...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named("authorizationmodel"+r.kinds.Suffix).
		For(r.kinds.NewAuthorizationModel(), r.kinds.AuthorizationModelEngageOptions()).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}
