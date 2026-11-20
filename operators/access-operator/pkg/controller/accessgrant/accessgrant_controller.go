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

// Package accessgrant hosts the AccessGrant controller — setup wiring
// plus the Reconciler that drives the /access handshake against
// user-side access SDKs.
package accessgrant

import (
	"fmt"

	"go.platform-mesh.io/access-operator/pkg/config"
	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"

	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

// ControllerName is the stable identifier used for the AccessGrant
// controller's metrics and leader-election keys.
const ControllerName = "AccessGrant"

// Controller wires the AccessGrant Reconciler into a multi-cluster
// manager backed by an apiexport-provider for access.platform-mesh.io.
type Controller struct {
	reconciler  *Reconciler
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
}

// NewController builds a Controller with production defaults. Tests
// that need to override the Reconciler's HTTP client or SA-token
// minter can reach through Reconciler() after construction.
func NewController(mgr mcmanager.Manager, cfg *config.OperatorConfig) (*Controller, error) {
	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating rate limiter: %w", err)
	}
	return &Controller{
		reconciler:  NewReconciler(mgr, cfg),
		rateLimiter: rl,
	}, nil
}

// Reconciler returns the underlying Reconciler so callers (tests,
// embedding operators) can tweak injectable dependencies.
func (c *Controller) Reconciler() *Reconciler {
	return c.reconciler
}

// SetupWithManager registers the AccessGrant controller on mgr.
func (c *Controller) SetupWithManager(mgr mcmanager.Manager, commonCfg *pmconfig.CommonServiceConfig, eventPredicates ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: commonCfg.MaxConcurrentReconciles,
		RateLimiter:             c.rateLimiter,
	}
	return mcbuilder.ControllerManagedBy(mgr).
		Named(ControllerName).
		For(&pmaccessv1alpha1.AccessGrant{}).
		WithOptions(opts).
		Complete(c.reconciler)
}
