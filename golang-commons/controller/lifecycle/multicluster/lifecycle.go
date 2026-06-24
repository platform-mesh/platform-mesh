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

package multicluster

import (
	"context"
	"fmt"
	"log"

	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/controller/lifecycle"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/api"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/conditions"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/runtimeobject"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/spread"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/subroutine"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

type ClusterGetter interface {
	GetCluster(ctx context.Context, clusterName multicluster.ClusterName) (cluster.Cluster, error)
}

type LifecycleManager struct {
	log                *logger.Logger
	mgr                ClusterGetter
	config             api.Config
	subroutines        []subroutine.Subroutine
	spreader           *spread.Spreader
	conditionsManager  *conditions.ConditionManager
	prepareContextFunc api.PrepareContextFunc
	rateLimiter        workqueue.TypedRateLimiter[mcreconcile.Request]
	terminator         string
	initializer        string
}

func NewLifecycleManager(subroutines []subroutine.Subroutine, operatorName string, controllerName string, mgr ClusterGetter, log *logger.Logger) *LifecycleManager {
	log = log.MustChildLoggerWithAttributes("operator", operatorName, "controller", controllerName)
	return &LifecycleManager{
		log:         log,
		mgr:         mgr,
		subroutines: subroutines,
		config: api.Config{
			OperatorName:   operatorName,
			ControllerName: controllerName,
		},
	}
}

func (l *LifecycleManager) Config() api.Config {
	return l.config
}
func (l *LifecycleManager) Log() *logger.Logger {
	return l.log
}
func (l *LifecycleManager) Subroutines() []subroutine.Subroutine {
	return l.subroutines
}
func (l *LifecycleManager) PrepareContextFunc() api.PrepareContextFunc {
	return l.prepareContextFunc
}
func (l *LifecycleManager) ConditionsManager() api.ConditionManager {
	// it is important to return nil unsted of a nil pointer to the interface to avoid misbehaving nil checks
	if l.conditionsManager == nil {
		return nil
	}
	return l.conditionsManager
}
func (l *LifecycleManager) Spreader() api.SpreadManager { // it is important to return nil unsted of a nil pointer to the interface to avoid misbehaving nil checks
	if l.spreader == nil {
		return nil
	}
	return l.spreader
}

func (l *LifecycleManager) Terminator() string {
	return l.terminator
}

func (l *LifecycleManager) Initializer() string {
	return l.initializer
}

func (l *LifecycleManager) Reconcile(ctx context.Context, req mcreconcile.Request, instance runtimeobject.RuntimeObject) (ctrl.Result, error) {
	cl, err := l.mgr.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get cluster: %w", err)
	}
	client := cl.GetClient()
	return lifecycle.Reconcile(ctx, req.NamespacedName, instance, client, l)
}
func (l *LifecycleManager) SetupWithManagerBuilder(mgr mcmanager.Manager, maxReconciles int, reconcilerName string, instance runtimeobject.RuntimeObject, debugLabelValue string, log *logger.Logger, eventPredicates ...predicate.Predicate) (*mcbuilder.Builder, error) {
	if err := lifecycle.ValidateInterfaces(instance, log, l); err != nil {
		return nil, err
	}

	if (l.ConditionsManager() != nil || l.Spreader() != nil) && l.Config().ReadOnly {
		return nil, fmt.Errorf("cannot use conditions or spread reconciles in read-only mode")
	}

	eventPredicates = append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(debugLabelValue)}, eventPredicates...)
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: maxReconciles,
	}

	if l.rateLimiter != nil {
		opts.RateLimiter = l.rateLimiter
	}

	return mcbuilder.ControllerManagedBy(mgr).
		Named(reconcilerName).
		For(instance).
		WithOptions(opts).
		WithEventFilter(predicate.And(eventPredicates...)), nil
}
func (l *LifecycleManager) SetupWithManager(mgr mcmanager.Manager, maxReconciles int, reconcilerName string, instance runtimeobject.RuntimeObject, debugLabelValue string, r mcreconcile.Reconciler, log *logger.Logger, eventPredicates ...predicate.Predicate) error {
	b, err := l.SetupWithManagerBuilder(mgr, maxReconciles, reconcilerName, instance, debugLabelValue, log, eventPredicates...)
	if err != nil {
		return err
	}

	return b.Complete(r)
}

// WithPrepareContextFunc allows to set a function that prepares the context before each reconciliation
// This can be used to add additional information to the context that is needed by the subroutines
// You need to return a new context and an OperatorError in case of an error
func (l *LifecycleManager) WithPrepareContextFunc(prepareFunction api.PrepareContextFunc) *LifecycleManager {
	l.prepareContextFunc = prepareFunction
	return l
}

// WithReadOnly allows to set the controller to read-only mode
// In read-only mode, the controller will not update the status of the instance
func (l *LifecycleManager) WithReadOnly() *LifecycleManager {
	l.config.ReadOnly = true
	return l
}

// WithSpreadingReconciles sets the LifecycleManager to spread out the reconciles
func (l *LifecycleManager) WithSpreadingReconciles() api.Lifecycle {
	l.spreader = spread.NewSpreader()
	return l
}

func (l *LifecycleManager) WithConditionManagement() api.Lifecycle {
	l.conditionsManager = conditions.NewConditionManager()
	return l
}

func (l *LifecycleManager) WithStaticThenExponentialRateLimiter(opts ...ratelimiter.Option) *LifecycleManager {
	rateLimiter, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig(opts...))
	if err != nil {
		log.Fatalf("rate limiter config error: %s", err)
	}
	l.rateLimiter = rateLimiter
	return l
}

func (l *LifecycleManager) WithTerminator(terminator string) *LifecycleManager {
	l.terminator = terminator
	return l
}

func (l *LifecycleManager) WithInitializer(initializer string) *LifecycleManager {
	l.initializer = initializer
	return l
}
