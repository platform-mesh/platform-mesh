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

// Package acceptapi implements the reconciler for [pmbrokerv1alpha1.AcceptAPI] resources.
package acceptapi

import (
	"context"
	"errors"
	"time"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const (
	// ControllerName is the name of the AcceptAPI controller and the
	// prefix for its subroutine condition types.
	ControllerName = "acceptapi"

	// DefaultRequeueInterval is used when Options.RequeueInterval is
	// unset.
	DefaultRequeueInterval = 10 * time.Second
)

// Options configures the AcceptAPI reconciler. Client construction is
// the operator's concern: the reconciler only asks WorkspaceClientFunc
// for clients by workspace path.
type Options struct {
	// VerificationTreeRoot is the kcp workspace path under which the broker creates its verification workspaces.
	// Required.
	VerificationTreeRoot string

	// WorkspaceClientFunc returns a client scoped to the workspace with the given path.
	// Required.
	WorkspaceClientFunc func(path string) (ctrlruntimeclient.Client, error)

	// ClusterFilter restricts which provider clusters the controller engages.
	// Optional, defaults to engaging all clusters.
	ClusterFilter mcbuilder.ClusterFilterFunc

	// RequeueInterval is the poll interval while waiting for workspaces and bindings to become ready.
	// Defaults to [DefaultRequeueInterval].
	RequeueInterval time.Duration
}

// Reconciler reconciles AcceptAPI objects.
type Reconciler struct {
	clusterFilter mcbuilder.ClusterFilterFunc
	lifecycle     *lifecycle.Lifecycle
}

// NewReconciler validates and defaults opts and returns a new AcceptAPI reconciler.
func NewReconciler(mgr mcmanager.Manager, opts Options) (*Reconciler, error) {
	if opts.VerificationTreeRoot == "" {
		return nil, errors.New("options: VerificationTreeRoot is required")
	}
	if opts.WorkspaceClientFunc == nil {
		return nil, errors.New("options: WorkspaceClientFunc is required")
	}
	if opts.RequeueInterval <= 0 {
		opts.RequeueInterval = DefaultRequeueInterval
	}

	lc := lifecycle.New(mgr, ControllerName,
		func() ctrlruntimeclient.Object { return &pmbrokerv1alpha1.AcceptAPI{} },
		&bindingVerifiedSubroutine{opts: opts},
	).WithConditions(conditions.NewManager())

	return &Reconciler{clusterFilter: opts.ClusterFilter, lifecycle: lc}, nil
}

// SetupWithManager registers the reconciler and its watches with the manager.
func (r *Reconciler) SetupWithManager(mgr mcmanager.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).
		Named(ControllerName).
		For(&pmbrokerv1alpha1.AcceptAPI{}, mcbuilder.WithClusterFilter(r.clusterFilter)).
		Complete(r)
}

// Reconcile delegates to the subroutines lifecycle.
func (r *Reconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}
