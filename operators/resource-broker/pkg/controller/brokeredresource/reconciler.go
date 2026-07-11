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

// Package brokeredresource implements the per-GVR reconciler for consumer
// resources served through the broker.
package brokeredresource

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	"go.platform-mesh.io/subroutines/lifecycle"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	crcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

const (
	// ControllerNamePrefix prefixes the per-GVR controller names.
	ControllerNamePrefix = "brokeredresource-"

	// DefaultRequeueInterval is used when Options.RequeueInterval is
	// unset.
	DefaultRequeueInterval = 10 * time.Second
)

// AcceptAPIRef is an AcceptAPI together with the provider cluster it lives in.
type AcceptAPIRef struct {
	// Cluster is the logical cluster name of the provider workspace.
	Cluster string

	// AcceptAPI is the offer published by the provider.
	AcceptAPI *pmbrokerv1alpha1.AcceptAPI
}

// Options configures a brokered resource reconciler.
type Options struct {
	// GVK is the kind of the brokered resource.
	// Required.
	// TODO(ntnn): Maybe resolve either this or GVR with a RESTMapper.
	GVK schema.GroupVersionKind

	// GVR is the resource of the brokered resource.
	// Required.
	// TODO(ntnn): Maybe resolve either this or GVK with a RESTMapper.
	GVR metav1.GroupVersionResource

	// StagingTreeRoot is the workspace path under which staging workspaces live.
	// Required.
	StagingTreeRoot string

	// WorkspaceClientFunc returns a client scoped to the workspace with the given path.
	// Required.
	WorkspaceClientFunc func(path string) (ctrlruntimeclient.Client, error)

	// CoordinationClient is a client for the coordination cluster holding Assignments.
	// Required.
	CoordinationClient ctrlruntimeclient.Client

	// ListAcceptAPIs returns all known AcceptAPIs with their provider
	// clusters.
	// Required.
	ListAcceptAPIs func(ctx context.Context) ([]AcceptAPIRef, error)

	// PickAcceptAPI chooses among matching AcceptAPIs.
	// Defaults to a uniformly random pick.
	PickAcceptAPI func(refs []AcceptAPIRef) AcceptAPIRef

	// ControllerName overrides the derived controller name. Set it when
	// registering the same GVR for multiple providers.
	// Defaults to a name derived from GVR.
	ControllerName string

	// ClusterFilter restricts which provider clusters the controller engages.
	// Optional, defaults to engaging all clusters.
	ClusterFilter mcbuilder.ClusterFilterFunc

	// RequeueInterval is the poll interval while waiting for assignment,
	// staging copy and status changes.
	// Defaults to [DefaultRequeueInterval].
	RequeueInterval time.Duration
}

// Reconciler reconciles consumer objects of a single brokered GVR.
type Reconciler struct {
	gvk           schema.GroupVersionKind
	name          string
	clusterFilter mcbuilder.ClusterFilterFunc
	lifecycle     *lifecycle.Lifecycle
}

// NewReconciler validates and defaults opts and returns a new brokered
// resource reconciler.
func NewReconciler(mgr mcmanager.Manager, opts Options) (*Reconciler, error) {
	if opts.GVK.Empty() {
		return nil, errors.New("options: GVK is required")
	}
	if opts.GVR.Resource == "" || opts.GVR.Version == "" {
		return nil, errors.New("options: GVR is required")
	}
	if opts.StagingTreeRoot == "" {
		return nil, errors.New("options: StagingTreeRoot is required")
	}
	if opts.WorkspaceClientFunc == nil {
		return nil, errors.New("options: WorkspaceClientFunc is required")
	}
	if opts.CoordinationClient == nil {
		return nil, errors.New("options: CoordinationClient is required")
	}
	if opts.ListAcceptAPIs == nil {
		return nil, errors.New("options: ListAcceptAPIs is required")
	}
	if opts.PickAcceptAPI == nil {
		opts.PickAcceptAPI = func(refs []AcceptAPIRef) AcceptAPIRef {
			return refs[rand.IntN(len(refs))]
		}
	}
	if opts.RequeueInterval <= 0 {
		opts.RequeueInterval = DefaultRequeueInterval
	}

	name := opts.ControllerName
	if name == "" {
		name = controllerName(opts.GVR)
	}
	gvk := opts.GVK

	lc := lifecycle.New(mgr, name,
		func() ctrlruntimeclient.Object {
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(gvk)
			return obj
		},
		&assignmentSubroutine{opts: opts},
		&copySubroutine{opts: opts},
		&relatedResourcesSubroutine{opts: opts},
	)

	return &Reconciler{gvk: gvk, name: name, clusterFilter: opts.ClusterFilter, lifecycle: lc}, nil
}

// SetupWithManager registers the reconciler and its watches with the manager.
func (r *Reconciler) SetupWithManager(mgr mcmanager.Manager) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(r.gvk)
	return mcbuilder.ControllerManagedBy(mgr).
		Named(r.name).
		WithOptions(crcontroller.TypedOptions[mcreconcile.Request]{
			// The multicluster builder does not propagate manager-level
			// controller options; forward the setting manually.
			SkipNameValidation: mgr.GetControllerOptions().SkipNameValidation,
		}).
		For(obj, mcbuilder.WithClusterFilter(r.clusterFilter)).
		Complete(r)
}

// Reconcile delegates to the subroutines lifecycle.
func (r *Reconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}

// controllerName derives the controller name from the brokered GVR.
func controllerName(gvr metav1.GroupVersionResource) string {
	name := ControllerNamePrefix + gvr.Resource
	if gvr.Group != "" {
		name += "." + gvr.Group
	}
	return name
}
