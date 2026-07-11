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

package broker

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	"go.platform-mesh.io/resource-broker/pkg/controller/brokeredresource"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/providers/multi"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

// resourceInfo identifies a brokered resource discovered from an APIExport.
type resourceInfo struct {
	GVK schema.GroupVersionKind
	GVR metav1.GroupVersionResource
}

// discovery reconciles APIExportEndpointSlices in the broker's kcp workspace.
// Each slice except the AcceptAPI one gets a multi-provider entry and a
// controller per resource served by its APIExport. Deleted slices have their
// provider removed; controllers cannot be removed from a running manager and
// stay idle until restart.
type discovery struct {
	log           logr.Logger
	client        ctrlruntimeclient.Client
	kcpConfig     *rest.Config
	scheme        *runtime.Scheme
	acceptAPIName string
	wcf           workspaceClientFn
	providers     *multi.Provider
	register      func(slice string, info resourceInfo) error

	registered map[string]map[metav1.GroupVersionResource]bool
}

// Reconcile ensures a provider and resource controllers for the slice.
func (d *discovery) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if req.Name == d.acceptAPIName {
		return ctrl.Result{}, nil
	}

	slice := &kcpapisv1alpha1.APIExportEndpointSlice{}
	if err := d.client.Get(ctx, types.NamespacedName{Name: req.Name}, slice); err != nil {
		if apierrors.IsNotFound(err) {
			d.removeProvider(req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting APIExportEndpointSlice %q: %w", req.Name, err)
	}

	if err := d.ensureProvider(slice.Name); err != nil {
		return ctrl.Result{}, err
	}

	infos, err := d.resolve(ctx, slice.Spec.APIExport)
	if err != nil {
		return ctrl.Result{}, err
	}

	if d.registered == nil {
		d.registered = map[string]map[metav1.GroupVersionResource]bool{}
	}
	if d.registered[slice.Name] == nil {
		d.registered[slice.Name] = map[metav1.GroupVersionResource]bool{}
	}

	current := map[metav1.GroupVersionResource]bool{}
	for _, info := range infos {
		current[info.GVR] = true
		if d.registered[slice.Name][info.GVR] {
			continue
		}
		if err := d.register(slice.Name, info); err != nil {
			return ctrl.Result{}, fmt.Errorf("registering controller for %s: %w", info.GVR, err)
		}
		d.registered[slice.Name][info.GVR] = true
		d.log.Info("registered brokered resource controller", "slice", slice.Name, "gvr", info.GVR)
	}

	for gvr := range d.registered[slice.Name] {
		if !current[gvr] {
			d.log.Info("brokered resource removed from APIExport, restart required to stop its controller", "slice", slice.Name, "gvr", gvr)
		}
	}

	return ctrl.Result{}, nil
}

// ensureProvider adds a provider for the slice if not yet present.
func (d *discovery) ensureProvider(sliceName string) error {
	if _, ok := d.providers.GetProvider(sliceName); ok {
		return nil
	}

	provider, err := apiexport.New(d.kcpConfig, sliceName, apiexport.Options{Scheme: d.scheme})
	if err != nil {
		return fmt.Errorf("creating provider for slice %q: %w", sliceName, err)
	}
	if err := d.providers.AddProvider(sliceName, provider); err != nil {
		return fmt.Errorf("adding provider for slice %q: %w", sliceName, err)
	}
	d.log.Info("added provider for APIExportEndpointSlice", "slice", sliceName)
	return nil
}

// removeProvider removes the provider of a deleted slice.
func (d *discovery) removeProvider(sliceName string) {
	if _, ok := d.providers.GetProvider(sliceName); !ok {
		return
	}
	d.providers.RemoveProvider(sliceName)
	d.log.Info("removed provider for deleted APIExportEndpointSlice", "slice", sliceName)
}

// resolve resolves the resources served by the referenced APIExport.
func (d *discovery) resolve(ctx context.Context, ref kcpapisv1alpha1.ExportBindingReference) ([]resourceInfo, error) {
	cl := d.client
	if ref.Path != "" {
		var err error
		cl, err = d.wcf(ref.Path)
		if err != nil {
			return nil, fmt.Errorf("building client for export path %q: %w", ref.Path, err)
		}
	}

	export := &kcpapisv1alpha2.APIExport{}
	if err := cl.Get(ctx, types.NamespacedName{Name: ref.Name}, export); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("getting APIExport %q: %w", ref.Name, err)
	}

	infos := make([]resourceInfo, 0, len(export.Spec.Resources))
	for _, res := range export.Spec.Resources {
		resourceSchema := &kcpapisv1alpha1.APIResourceSchema{}
		if err := cl.Get(ctx, types.NamespacedName{Name: res.Schema}, resourceSchema); err != nil {
			return nil, fmt.Errorf("getting APIResourceSchema %q: %w", res.Schema, err)
		}

		version := storageVersion(resourceSchema)
		if version == "" {
			return nil, fmt.Errorf("APIResourceSchema %q has no storage version", res.Schema)
		}

		infos = append(infos, resourceInfo{
			GVK: schema.GroupVersionKind{Group: res.Group, Version: version, Kind: resourceSchema.Spec.Names.Kind},
			GVR: metav1.GroupVersionResource{Group: res.Group, Version: version, Resource: resourceSchema.Spec.Names.Plural},
		})
	}

	return infos, nil
}

// storageVersion returns the storage version of the resource schema.
func storageVersion(resourceSchema *kcpapisv1alpha1.APIResourceSchema) string {
	for _, v := range resourceSchema.Spec.Versions {
		if v.Storage {
			return v.Name
		}
	}
	return ""
}

// registerBrokeredResource returns a func creating and registering a brokered
// resource controller with the manager.
func registerBrokeredResource(mgr mcmanager.Manager, opts Options, wcf workspaceClientFn, coordinationClient ctrlruntimeclient.Client, acceptAPIProvider *apiexport.Provider) func(slice string, info resourceInfo) error {
	return func(slice string, info resourceInfo) error {
		name := brokeredresource.ControllerNamePrefix + slice + "-" + info.GVR.Resource
		if info.GVR.Group != "" {
			name += "." + info.GVR.Group
		}

		reconciler, err := brokeredresource.NewReconciler(mgr, brokeredresource.Options{
			GVK:                 info.GVK,
			GVR:                 info.GVR,
			StagingTreeRoot:     opts.StagingTreeRoot,
			WorkspaceClientFunc: wcf,
			CoordinationClient:  coordinationClient,
			ListAcceptAPIs:      listAcceptAPIs(acceptAPIProvider.Lister()),
			ControllerName:      name,
			ClusterFilter:       providerClusters(slice),
		})
		if err != nil {
			return fmt.Errorf("creating reconciler: %w", err)
		}
		return reconciler.SetupWithManager(mgr)
	}
}
