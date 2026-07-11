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

// Package assignment implements the reconciler for [pmcoordbrokerv1alpha1.Assignment] resources.
package assignment

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
	// ControllerName is the name of the Assignment controller.
	ControllerName = "assignment"

	// DefaultRequeueInterval is used when Options.RequeueInterval is
	// unset.
	DefaultRequeueInterval = 10 * time.Second
)

// Options configures the Assignment reconciler.
type Options struct {
	// WorkspaceClientFunc returns a client scoped to the workspace with the given path.
	// Required.
	WorkspaceClientFunc func(path string) (ctrlruntimeclient.Client, error)

	// RequeueInterval is the poll interval while waiting for the staging workspace to become ready.
	// Defaults to [DefaultRequeueInterval].
	RequeueInterval time.Duration
}

// Reconciler reconciles Assignment objects.
type Reconciler struct {
	lifecycle *lifecycle.Lifecycle
}

// NewReconciler validates and defaults opts and returns a new Assignment reconciler.
func NewReconciler(mgr mcmanager.Manager, opts Options) (*Reconciler, error) {
	if opts.WorkspaceClientFunc == nil {
		return nil, errors.New("options: WorkspaceClientFunc is required")
	}
	if opts.RequeueInterval <= 0 {
		opts.RequeueInterval = DefaultRequeueInterval
	}

	lc := lifecycle.New(mgr, ControllerName,
		func() ctrlruntimeclient.Object { return &pmcoordbrokerv1alpha1.Assignment{} },
		&stagingWorkspaceReadySubroutine{opts: opts},
	).WithConditions(conditions.NewManager())

	return &Reconciler{lifecycle: lc}, nil
}

// SetupWithManager registers the reconciler and its watches with the manager.
func (r *Reconciler) SetupWithManager(mgr mcmanager.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).
		Named(ControllerName).
		For(&pmcoordbrokerv1alpha1.Assignment{}).
		Complete(r)
}

// Reconcile delegates to the subroutines lifecycle.
func (r *Reconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}
