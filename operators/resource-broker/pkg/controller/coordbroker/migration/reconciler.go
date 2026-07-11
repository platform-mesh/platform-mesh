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

// Package migration implements the reconciler for [pmcoordbrokerv1alpha1.Migration] resources.
package migration

import (
	"context"
	"errors"
	"time"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	crcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const (
	// ControllerName is the name of the Migration controller.
	ControllerName = "migration"

	// DefaultRequeueInterval is used when Options.RequeueInterval is
	// unset.
	DefaultRequeueInterval = 10 * time.Second

	// DefaultStageNamespace is used when Options.StageNamespace is
	// unset.
	DefaultStageNamespace = "default"
)

// Options configures the Migration reconciler.
type Options struct {
	// ComputeClient is the client for the compute cluster stage
	// templates are deployed to.
	// Required.
	ComputeClient ctrlruntimeclient.Client

	// WorkspaceClientFunc returns a client scoped to the workspace with the given path.
	// Required.
	WorkspaceClientFunc func(path string) (ctrlruntimeclient.Client, error)

	// StagingTreeRoot is the workspace path staging workspaces are
	// created under.
	// Required.
	StagingTreeRoot string

	// StageNamespace is the namespace stage templates are deployed to
	// in the compute cluster.
	// Defaults to [DefaultStageNamespace].
	StageNamespace string

	// ClusterFilter restricts which provider clusters the controller engages.
	// Optional, defaults to engaging all clusters.
	ClusterFilter mcbuilder.ClusterFilterFunc

	// RequeueInterval is the poll interval while waiting for staging
	// workspaces and stages.
	// Defaults to [DefaultRequeueInterval].
	RequeueInterval time.Duration
}

// Reconciler reconciles Migration objects.
type Reconciler struct {
	clusterFilter mcbuilder.ClusterFilterFunc
	lifecycle     *lifecycle.Lifecycle
}

// NewReconciler validates and defaults opts and returns a new Migration reconciler.
func NewReconciler(mgr mcmanager.Manager, opts Options) (*Reconciler, error) {
	if opts.ComputeClient == nil {
		return nil, errors.New("options: ComputeClient is required")
	}
	if opts.WorkspaceClientFunc == nil {
		return nil, errors.New("options: WorkspaceClientFunc is required")
	}
	if opts.StagingTreeRoot == "" {
		return nil, errors.New("options: StagingTreeRoot is required")
	}
	if opts.StageNamespace == "" {
		opts.StageNamespace = DefaultStageNamespace
	}
	if opts.RequeueInterval <= 0 {
		opts.RequeueInterval = DefaultRequeueInterval
	}

	lc := lifecycle.New(mgr, ControllerName,
		func() ctrlruntimeclient.Object { return &pmcoordbrokerv1alpha1.Migration{} },
		&stagesSubroutine{opts: opts},
		&cutoverSubroutine{opts: opts},
	).WithConditions(conditions.NewManager())

	return &Reconciler{clusterFilter: opts.ClusterFilter, lifecycle: lc}, nil
}

// SetupWithManager registers the reconciler and its watches with the manager.
func (r *Reconciler) SetupWithManager(mgr mcmanager.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).
		Named(ControllerName).
		WithOptions(crcontroller.TypedOptions[mcreconcile.Request]{
			// The multicluster builder does not propagate manager-level
			// controller options; forward the setting manually.
			SkipNameValidation: mgr.GetControllerOptions().SkipNameValidation,
		}).
		For(&pmcoordbrokerv1alpha1.Migration{}, mcbuilder.WithClusterFilter(r.clusterFilter)).
		Complete(r)
}

// Reconcile delegates to the subroutines lifecycle.
func (r *Reconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}
