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

	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"k8s.io/client-go/util/workqueue"

	"go.platform-mesh.io/account-operator/internal/config"
	"go.platform-mesh.io/account-operator/pkg/subroutines/finalizeaccountinfo"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/lifecycle"
)

const accountInfoReconcilerName = "AccountInfoReconciler"

// AccountInfoReconciler orchestrates AccountInfo resources across logical clusters.
type AccountInfoReconciler struct {
	cfg         config.OperatorConfig
	lifecycle   *lifecycle.Lifecycle
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
}

func NewAccountInfoReconciler(log *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig) (*AccountInfoReconciler, error) {
	subs := []subroutines.Subroutine{}

	if cfg.Controllers.AccountInfo.Enabled {
		faSub, err := finalizeaccountinfo.New(mgr)
		if err != nil {
			return nil, fmt.Errorf("creating FinalizeAccountInfo subroutine: %w", err)
		}
		subs = append(subs, faSub)
	}

	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	lc := lifecycle.New(mgr, accountInfoReconcilerName, func() client.Object {
		return &corev1alpha1.AccountInfo{}
	}, subs...)

	return &AccountInfoReconciler{
		cfg:         cfg,
		lifecycle:   lc,
		rateLimiter: rl,
	}, nil
}

func (r *AccountInfoReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformmeshconfig.CommonServiceConfig, log *logger.Logger, eventPredicates ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		RateLimiter:             r.rateLimiter,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, eventPredicates...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named(accountInfoReconcilerName).
		For(&corev1alpha1.AccountInfo{}).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}

func (r *AccountInfoReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}
