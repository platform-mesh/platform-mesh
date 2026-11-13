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

// Package engine drives composition per consumer workspace: compile an RGD, publish its
// type as a bound API, and reconcile instances as the operator's own kcp identity.
package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/dynamiccontroller"
	"github.com/kubernetes-sigs/kro/pkg/graph/revisions"
	krometadata "github.com/kubernetes-sigs/kro/pkg/metadata"

	"go.platform-mesh.io/kro-composition-operator/internal/composition"
	"go.platform-mesh.io/kro-composition-operator/internal/workspace"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/util/retry"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

var ccGVK = schema.GroupVersionKind{Group: "ui.platform-mesh.io", Version: "v1alpha1", Kind: "ContentConfiguration"}

var (
	errNotEstablished   = fmt.Errorf("composite CRD not established yet")
	errTeardownPending  = fmt.Errorf("composite teardown pending")
	errInstancesPending = fmt.Errorf("composite instances still draining")
)

// rgdFinalizer holds the RGD while teardown runs in order, ahead of owner-ref GC.
const rgdFinalizer = "kro-composition.platform-mesh.io/teardown"

// conditionTypePortalQueryable is False when the API group is not a valid GraphQL
// identifier. The type still serves over kubectl, it just gets no portal entry.
// Domain-qualified because it is ours, not kro's.
const conditionTypePortalQueryable krov1alpha1.ConditionType = "kro-composition.platform-mesh.io/PortalQueryable"

// instanceRequeueInterval matches kro's own instance reconciler default.
const instanceRequeueInterval = 3 * time.Second

// transientError marks a self-resolving condition. Callers requeue quietly.
type transientError struct{ err error }

func (e transientError) Error() string { return e.err.Error() }
func (e transientError) Unwrap() error { return e.err }

func transient(err error) error { return transientError{err: err} }

// IsTransient reports whether err is an expected, self-resolving requeue signal.
func IsTransient(err error) bool {
	var t transientError
	return errors.As(err, &t)
}

// Engine holds per-workspace state: compiled graphs and one kro DynamicController each.
type Engine struct {
	Workspaces *workspace.Provider
	DCConfig   dynamiccontroller.Config

	// rootCtx bounds the lifetime of per-workspace dynamic-controller goroutines.
	rootCtx context.Context

	graphs  sync.Map // "cluster|group/version/resource" -> *graph.Graph
	rgdGVRs sync.Map // "cluster|rgdName" -> schema.GroupVersionResource (for delete cleanup)

	mu  sync.Mutex
	dcs map[string]*wsController // clusterName -> per-workspace controller
}

type wsController struct {
	dc *dynamiccontroller.DynamicController

	// Per workspace, because kro's registry keys on the RGD name alone.
	revisions *revisions.Registry
}

// New returns an Engine. rootCtx must outlive all workspaces (the manager's ctx).
func New(rootCtx context.Context, ws *workspace.Provider, dcConfig dynamiccontroller.Config) *Engine {
	return &Engine{Workspaces: ws, DCConfig: dcConfig, rootCtx: rootCtx, dcs: map[string]*wsController{}}
}

// workspaceTerminating reports whether the workspace itself is being deleted, which puts
// teardown in force mode. Unreadable means assume healthy.
func workspaceTerminating(ctx context.Context, c ctrlruntimeclient.Client) bool {
	lc := &kcpcorev1alpha1.LogicalCluster{}
	if err := c.Get(ctx, types.NamespacedName{Name: "cluster"}, lc); err != nil {
		return false
	}
	return !lc.DeletionTimestamp.IsZero()
}

// ReconcileRGD compiles one RGD, publishes its type, and registers the instance watch.
func (e *Engine) ReconcileRGD(ctx context.Context, clusterName, rgdName string) error {
	log := logf.FromContext(ctx).WithValues("cluster", clusterName, "rgd", rgdName)

	wc, err := e.Workspaces.For(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("workspace clients: %w", err)
	}

	rgd := &krov1alpha1.ResourceGraphDefinition{}
	if err := wc.Client.Get(ctx, types.NamespacedName{Name: rgdName}, rgd); err != nil {
		if apierrors.IsNotFound(err) {
			return e.handleRGDDeleted(clusterName, rgdName)
		}
		return err
	}

	// Tear down in order, then release our finalizer so the RGD can be collected.
	if !rgd.DeletionTimestamp.IsZero() {
		force := workspaceTerminating(ctx, wc.Client)

		// Drain while the type is still served. Nothing cascades when an APIBinding goes, so
		// instances would strand behind an unserved API holding kro's finalizer.
		if gvr, known := e.instanceGVRFor(wc, clusterName, rgd); known {
			drained, err := drainInstances(ctx, wc.Metadata, wc.Dynamic, gvr, force)
			if err != nil {
				return err
			}
			if !drained {
				return transient(errInstancesPending)
			}
		} else {
			// Finish teardown rather than hold the RGD forever, stranding instances.
			log.Info("cannot determine the instance type; tearing down without draining instances")
		}

		done, err := teardownComposite(ctx, wc.Client, rgd, force)
		if err != nil {
			return err
		}
		if !done {
			return transient(errTeardownPending)
		}
		if err := e.handleRGDDeleted(clusterName, rgdName); err != nil {
			return err
		}
		if controllerutil.RemoveFinalizer(rgd, rgdFinalizer) {
			if err := wc.Client.Update(ctx, rgd); err != nil {
				return err
			}
		}
		return nil
	}

	// Hold the RGD via a finalizer so we control teardown ordering on delete.
	if controllerutil.AddFinalizer(rgd, rgdFinalizer) {
		if err := wc.Client.Update(ctx, rgd); err != nil {
			return transient(fmt.Errorf("add finalizer: %w", err))
		}
	}

	compiler, err := composition.NewCompiler(wc.Config)
	if err != nil {
		return fmt.Errorf("compiler: %w", err)
	}
	g, err := compiler.Compile(rgd)
	if err != nil {
		// Usually aggregation lag or an API not bound yet, so keep retrying. Reported on the
		// RGD too: a reference that never resolves looks identical from here.
		notCompilable := fmt.Errorf("RGD not compilable yet: %w", err)
		if statusErr := writeRGDGraphInvalid(ctx, wc.Client, rgd.Name, notCompilable.Error()); statusErr != nil {
			return statusErr
		}
		return transient(notCompilable)
	}
	if g.CRD == nil {
		return fmt.Errorf("kro produced no CRD for RGD %q", rgdName)
	}

	// A group the portal cannot query still yields a perfectly usable API over kubectl, so
	// the type is published either way. What gets skipped is the portal wiring, and the
	// reason is reported on the RGD. See ensureContentConfiguration and the PortalQueryable
	// condition below.
	portalErr := composition.ValidateAPIGroup(g.CRD.Spec.Group)
	if portalErr != nil {
		log.Info("composite type will not be shown in the portal", "reason", portalErr.Error())
	}

	// Publish the composite type as a *bound* API (APIResourceSchema + per-RGD
	// APIExport + self-binding) rather than a plain reflected CRD, so the graphql
	// gateway and security-operator (both boundResource-driven) treat it as first-class.
	bound, err := e.publishComposite(ctx, wc.Client, g.CRD, rgd)
	if err != nil {
		// A refused schema change is terminal: the previously published schema stays served
		// and only an RGD edit, which re-triggers this reconcile on its own, can resolve it.
		// Requeuing would just repeat the same refusal.
		if isBreakingSchemaChange(err) {
			log.Info("refusing to republish the composite type", "reason", err.Error())
			return writeRGDKindNotReady(ctx, wc.Client, rgd.Name, err.Error())
		}
		// A type conflict is different: deleting the RGD holding the type resolves it without
		// touching this one, and that does not reconcile this RGD. So report it and keep
		// retrying. The status write is skipped while the reason is unchanged.
		if isTypeConflict(err) {
			if statusErr := writeRGDKindNotReady(ctx, wc.Client, rgd.Name, err.Error()); statusErr != nil {
				return statusErr
			}
			return transient(err)
		}
		return err
	}
	if !bound {
		return transient(errNotEstablished)
	}

	gvr := instanceGVR(g.CRD)
	e.graphs.Store(graphKey(clusterName, gvr), g)
	e.rgdGVRs.Store(rgdName+"|"+clusterName, gvr)

	// Emit a portal ContentConfiguration for the generated type (best-effort: skips
	// where the ui.platform-mesh.io API isn't served, e.g. no portal). Skipped entirely for
	// a group the portal cannot query, where the nav entry would only lead to a failing
	// query.
	if portalErr == nil {
		if err := e.ensureContentConfiguration(ctx, wc, rgd, g.CRD); err != nil {
			return err
		}
	} else if err := e.deleteContentConfiguration(ctx, wc, rgd); err != nil {
		// An RGD edited from a queryable group to an unqueryable one would otherwise keep
		// its old nav entry, pointing at a type that no longer exists.
		return err
	}

	wsc := e.ensureController(clusterName, wc)
	if err := wsc.dc.Register(ctx, gvr, e.instanceHandler(clusterName, wc, rgd, g, wsc.revisions, wsc.dc)); err != nil {
		return transient(fmt.Errorf("register instance watch %s: %w", gvr, err))
	}

	// Report the RGD as Active (state + topological order + readiness conditions) so
	// the portal and kubectl show it serving, now that the CRD and controller are up. A
	// non-nil portalErr additionally records PortalQueryable=False with the reason. The
	// type is still Active because the API itself works.
	if err := writeRGDStatus(ctx, wc.Client, rgd.Name, statusFromGraph(g), portalErr); err != nil {
		return transient(fmt.Errorf("write RGD status: %w", err))
	}

	log.Info("composite type ready and watched", "gvr", gvr.String())
	return nil
}

// ensureContentConfiguration writes the portal nav entry for the type, owned by the RGD.
// Best-effort: skips where the ui.platform-mesh.io API is not served.
func (e *Engine) ensureContentConfiguration(ctx context.Context, wc *workspace.Clients, rgd *krov1alpha1.ResourceGraphDefinition, crd *apiextensionsv1.CustomResourceDefinition) error {
	version := storageVersion(crd)
	content := composition.BuildContentConfig(
		crd.Spec.Group, version, crd.Spec.Names.Kind, crd.Spec.Names.Plural,
		crd.Spec.Scope == apiextensionsv1.NamespaceScoped, specFieldsFromCRD(crd, version),
	)

	cc := &unstructured.Unstructured{}
	cc.SetGroupVersionKind(ccGVK)
	cc.SetName(contentConfigName(rgd.Name))

	yes := true
	_, err := controllerutil.CreateOrUpdate(ctx, wc.Client, cc, func() error {
		cc.SetLabels(map[string]string{"ui.platform-mesh.io/entity": composition.AccountEntity})
		cc.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion:         krov1alpha1.GroupVersion.String(),
			Kind:               "ResourceGraphDefinition",
			Name:               rgd.Name,
			UID:                rgd.UID,
			BlockOwnerDeletion: &yes,
		}})
		return unstructured.SetNestedMap(cc.Object,
			map[string]any{"contentType": "json", "content": content},
			"spec", "inlineConfiguration")
	})
	if err != nil {
		if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
			logf.FromContext(ctx).V(1).Info("ContentConfiguration API not served; skipping portal config", "gvr", ccGVK.String())
			return nil
		}
		return fmt.Errorf("ensure ContentConfiguration: %w", err)
	}
	return nil
}

// handleRGDDeleted stops watching the composite type and drops caches.
func (e *Engine) handleRGDDeleted(clusterName, rgdName string) error {
	v, ok := e.rgdGVRs.LoadAndDelete(rgdName + "|" + clusterName)
	if !ok {
		return nil // never fully reconciled; nothing to clean up
	}
	gvr := v.(schema.GroupVersionResource)
	e.graphs.Delete(graphKey(clusterName, gvr))

	e.mu.Lock()
	wsc, running := e.dcs[clusterName]
	e.mu.Unlock()
	if running {
		if err := wsc.dc.Deregister(context.Background(), gvr); err != nil {
			return fmt.Errorf("deregister %s: %w", gvr, err)
		}
	}
	logf.Log.WithName("engine").Info("RGD deleted; stopped watching composite type", "cluster", clusterName, "rgd", rgdName, "gvr", gvr.String())
	return nil
}

// ensureController lazily creates and starts a kro DynamicController for a workspace.
func (e *Engine) ensureController(clusterName string, wc *workspace.Clients) *wsController {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c, ok := e.dcs[clusterName]; ok {
		return c
	}
	dc := dynamiccontroller.NewDynamicController(
		logf.Log.WithName("dynamic-controller").WithValues("cluster", clusterName),
		e.DCConfig,
		wc.Metadata,
		wc.Mapper,
	)
	c := &wsController{dc: dc, revisions: revisions.NewRegistry()}
	e.dcs[clusterName] = c
	go func() {
		if err := dc.Start(e.rootCtx); err != nil && e.rootCtx.Err() == nil {
			logf.Log.WithName("engine").Error(err, "workspace dynamic controller stopped", "cluster", clusterName)
		}
	}()
	return c
}

// instanceGVRFor resolves the instance type, compiling the RGD if the in-memory cache
// misses. A restart would otherwise skip the drain for an already-deleting RGD.
func (e *Engine) instanceGVRFor(wc *workspace.Clients, clusterName string, rgd *krov1alpha1.ResourceGraphDefinition) (schema.GroupVersionResource, bool) {
	if v, ok := e.rgdGVRs.Load(rgd.Name + "|" + clusterName); ok {
		return v.(schema.GroupVersionResource), true
	}
	compiler, err := composition.NewCompiler(wc.Config)
	if err != nil {
		return schema.GroupVersionResource{}, false
	}
	g, err := compiler.Compile(rgd)
	if err != nil || g.CRD == nil {
		return schema.GroupVersionResource{}, false
	}
	return instanceGVR(g.CRD), true
}

// drainInstances deletes every instance of gvr and reports whether they are all gone.
// Runs before the type is unserved, so kro's cleanup still can.
//
// force stops waiting when the workspace is terminating, which would otherwise block
// workspace and account deletion.
func drainInstances(ctx context.Context, metaCl metadata.Interface, dyn dynamic.Interface, gvr schema.GroupVersionResource, force bool) (bool, error) {
	// Metadata-only: names, deletion timestamps and finalizers are all this needs, and an
	// instance's spec and status can be large.
	list, err := metaCl.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return true, nil // type already gone, nothing to drain
		}
		return false, fmt.Errorf("list instances of %s: %w", gvr.Resource, err)
	}
	if len(list.Items) == 0 {
		return true, nil
	}

	for i := range list.Items {
		inst := &list.Items[i]
		ri := dyn.Resource(gvr).Namespace(inst.Namespace)

		if inst.DeletionTimestamp.IsZero() {
			if err := ri.Delete(ctx, inst.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				return false, fmt.Errorf("delete instance %s: %w", inst.Name, err)
			}
		}
		// The listed metadata already says whether there is a finalizer to strip, so skip the
		// read-modify-write for the instances that carry none.
		if force && krometadata.HasInstanceFinalizer(inst) {
			if err := releaseInstanceFinalizer(ctx, ri, inst.Name); err != nil {
				return false, fmt.Errorf("release finalizer on %s: %w", inst.Name, err)
			}
		}
	}

	// Deletes are issued. kro's instance reconciler still has to delete the children and
	// release its finalizer, so report not-drained and let the caller requeue. In force mode
	// nothing holds the instances any more, so do not hold the RGD either.
	return force, nil
}

// releaseInstanceFinalizer strips kro's finalizer, which nothing can process any more.
func releaseInstanceFinalizer(ctx context.Context, ri dynamic.ResourceInterface, name string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur, err := ri.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !krometadata.HasInstanceFinalizer(cur) {
			return nil
		}
		krometadata.RemoveInstanceFinalizer(cur)
		_, err = ri.Update(ctx, cur, metav1.UpdateOptions{})
		return err
	})
}

// deleteContentConfiguration removes the portal nav entry, tolerating it being absent.
func (e *Engine) deleteContentConfiguration(ctx context.Context, wc *workspace.Clients, rgd *krov1alpha1.ResourceGraphDefinition) error {
	cc := &unstructured.Unstructured{}
	cc.SetGroupVersionKind(ccGVK)
	cc.SetName(contentConfigName(rgd.Name))

	if err := wc.Client.Delete(ctx, cc); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("delete ContentConfiguration: %w", err)
	}
	return nil
}
