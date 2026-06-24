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

	"go.platform-mesh.io/apis/terminal/v1alpha1"
	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"
	"go.platform-mesh.io/terminal-controller-manager/internal/config"
	tcmsubroutines "go.platform-mesh.io/terminal-controller-manager/pkg/subroutines"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const (
	terminalReconcilerName = "TerminalReconciler"
)

// TerminalReconciler orchestrates Terminal resources across logical clusters.
type TerminalReconciler struct {
	cfg       config.OperatorConfig
	lifecycle *lifecycle.Lifecycle
}

func NewTerminalReconciler(_ *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig, runtimeClient client.Client) *TerminalReconciler { // coverage-ignore
	subs := []subroutines.Subroutine{}

	// Lifetime subroutine runs first to check for expired terminals
	if cfg.Subroutines.Lifetime.Enabled {
		subs = append(subs, tcmsubroutines.NewLifetimeSubroutine(mgr, cfg.Terminal.Lifetime))
	}

	if cfg.Subroutines.Pod.Enabled {
		subs = append(subs, tcmsubroutines.NewPodSubroutine(
			mgr,
			runtimeClient,
			cfg.Terminal.Image,
			cfg.Terminal.Namespace,
			cfg.Terminal.HostAliasIP,
			cfg.Terminal.HostAliasNames,
		))
	}

	if cfg.Subroutines.Service.Enabled {
		subs = append(subs, tcmsubroutines.NewServiceSubroutine(runtimeClient, cfg.Terminal.Namespace))
	}

	if cfg.Subroutines.HTTPRoute.Enabled {
		subs = append(subs, tcmsubroutines.NewHTTPRouteSubroutine(
			runtimeClient,
			cfg.Terminal.Namespace,
			cfg.Gateway.Name,
			cfg.Gateway.Namespace,
			cfg.Gateway.Hostnames,
		))
	}

	return &TerminalReconciler{
		cfg: cfg,
		lifecycle: lifecycle.New(mgr, terminalReconcilerName, func() client.Object {
			return &v1alpha1.Terminal{}
		}, subs...).WithConditions(conditions.NewManager()),
	}
}

func (r *TerminalReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformmeshconfig.CommonServiceConfig, _ *logger.Logger, eventPredicates ...predicate.Predicate) error { // coverage-ignore
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, eventPredicates...)

	return mcbuilder.ControllerManagedBy(mgr).
		Named("terminal").
		For(&v1alpha1.Terminal{}).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}

func (r *TerminalReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) { // coverage-ignore
	return r.lifecycle.Reconcile(ctx, req)
}
