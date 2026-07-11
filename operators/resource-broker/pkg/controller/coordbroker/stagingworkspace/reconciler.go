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

// Package stagingworkspace implements the reconciler for [pmcoordbrokerv1alpha1.StagingWorkspace] resources.
package stagingworkspace

import (
	"context"
	"errors"
	"time"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const (
	// ControllerName is the name of the StagingWorkspace controller.
	ControllerName = "stagingworkspace"

	// DefaultRequeueInterval is used when Options.RequeueInterval is
	// unset.
	DefaultRequeueInterval = 10 * time.Second
)

// Options configures the StagingWorkspace reconciler.
type Options struct {
	// StagingTreeRoot is the kcp workspace path under which the broker creates its staging workspaces.
	// Required.
	StagingTreeRoot string

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

// Reconciler reconciles StagingWorkspace objects.
type Reconciler struct {
	clusterFilter mcbuilder.ClusterFilterFunc
	lifecycle     *lifecycle.Lifecycle
}

// NewReconciler validates and defaults opts and returns a new StagingWorkspace reconciler.
func NewReconciler(mgr mcmanager.Manager, opts Options) (*Reconciler, error) {
	if opts.StagingTreeRoot == "" {
		return nil, errors.New("options: StagingTreeRoot is required")
	}
	if opts.WorkspaceClientFunc == nil {
		return nil, errors.New("options: WorkspaceClientFunc is required")
	}
	if opts.RequeueInterval <= 0 {
		opts.RequeueInterval = DefaultRequeueInterval
	}

	lc := lifecycle.New(mgr, ControllerName,
		func() ctrlruntimeclient.Object { return &pmcoordbrokerv1alpha1.StagingWorkspace{} },
		&workspaceReadySubroutine{opts: opts},
		&bindingReadySubroutine{opts: opts},
	).WithConditions(conditions.NewManager())

	return &Reconciler{clusterFilter: opts.ClusterFilter, lifecycle: lc}, nil
}

// SetupWithManager registers the reconciler and its watches with the manager.
func (r *Reconciler) SetupWithManager(mgr mcmanager.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).
		Named(ControllerName).
		For(&pmcoordbrokerv1alpha1.StagingWorkspace{}, mcbuilder.WithClusterFilter(r.clusterFilter)).
		Complete(r)
}

// Reconcile delegates to the subroutines lifecycle.
func (r *Reconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}
