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

// Package kapply provides a pure-Go equivalent of `kubectl apply -k <dir>`,
// building a Kustomize stack and applying it via Server-Side Apply (SSA).
package kapply

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/yaml"
)

// Clients bundles the cluster interfaces this package needs.
type Clients struct {
	Dynamic   dynamic.Interface
	Discovery discovery.DiscoveryInterface
	Mapper    meta.RESTMapper
}

// NewClients creates Clients from a *rest.Config (handy if you don’t already
// have the dynamic/discovery/mapper wired up).
func NewClients(cfg *rest.Config) (Clients, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return Clients{}, fmt.Errorf("dynamic client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return Clients{}, fmt.Errorf("discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(disco))
	return Clients{Dynamic: dyn, Discovery: disco, Mapper: mapper}, nil
}

// Options controls ApplyDir behavior.
type Options struct {
	// FieldManager identifies your applier in managedFields (kubectl default is "kubectl").
	FieldManager string
	// ForceConflicts mirrors `--force-conflicts` (take ownership when needed).
	ForceConflicts bool
	// DefaultNamespace fills in a namespace for namespaced resources that
	// don’t have one set by Kustomize.
	DefaultNamespace string
	// PreapplyKinds are applied in a first pass to satisfy dependencies
	// (default: Namespace, CustomResourceDefinition).
	PreapplyKinds []string
	// DryRun prints/introspects without persisting (server-side dry-run).
	DryRun bool
	// WaitAfterCRDs pauses briefly and refreshes discovery after CRDs are applied.
	WaitAfterCRDs time.Duration
}

// Option is a functional option for ApplyDir.
type Option func(*Options)

func WithFieldManager(m string) Option         { return func(o *Options) { o.FieldManager = m } }
func WithForceConflicts(b bool) Option         { return func(o *Options) { o.ForceConflicts = b } }
func WithDefaultNamespace(ns string) Option    { return func(o *Options) { o.DefaultNamespace = ns } }
func WithDryRun() Option                       { return func(o *Options) { o.DryRun = true } }
func WithWaitAfterCRDs(d time.Duration) Option { return func(o *Options) { o.WaitAfterCRDs = d } }

// ApplyDir builds the Kustomize stack in dir and applies all objects via SSA.
func ApplyDir(ctx context.Context, dir string, c Clients, opts ...Option) error {
	o := &Options{
		FieldManager:   "kapply",
		ForceConflicts: true,
		PreapplyKinds:  []string{"Namespace", "CustomResourceDefinition"},
		WaitAfterCRDs:  2 * time.Second,
		DryRun:         false,
	}
	for _, fn := range opts {
		fn(o)
	}

	// 1) Build kustomize
	fs := filesys.MakeFsOnDisk()
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := k.Run(fs, dir)
	if err != nil {
		return fmt.Errorf("kustomize build failed: %w", err)
	}

	// 2) Pre-apply important kinds (e.g., Namespace, CRD)
	if err := applyKindsFirst(ctx, resMap, c, o); err != nil {
		return err
	}

	// 3) Apply everything (SSA)
	for _, r := range resMap.Resources() {
		yamlBytes, err := r.AsYAML()
		if err != nil {
			return fmt.Errorf("failed to get YAML for resource %s/%s: %w", r.GetGvk().Kind, r.GetName(), err)
		}
		if err := applyOne(ctx, string(yamlBytes), c, o); err != nil {
			return err
		}
	}
	return nil
}

func applyKindsFirst(ctx context.Context, rm resmap.ResMap, c Clients, o *Options) error {
	if len(o.PreapplyKinds) == 0 {
		return nil
	}
	kindSet := make(map[string]struct{}, len(o.PreapplyKinds))
	for _, k := range o.PreapplyKinds {
		kindSet[k] = struct{}{}
	}
	for _, r := range rm.Resources() {
		kind := r.GetGvk().Kind
		if _, wanted := kindSet[kind]; !wanted {
			continue
		}
		yamlBytes, err := r.AsYAML()
		if err != nil {
			return fmt.Errorf("failed to get YAML for resource %s/%s: %w", kind, r.GetName(), err)
		}
		if err := applyOne(ctx, string(yamlBytes), c, o); err != nil {
			return fmt.Errorf("pre-apply %s/%s: %w", kind, r.GetName(), err)
		}
	}
	// If CRDs were applied, give the API server a moment and refresh mapper cache.
	if _, hadCRD := kindSet["CustomResourceDefinition"]; hadCRD && o.WaitAfterCRDs > 0 {
		time.Sleep(o.WaitAfterCRDs)
		// Best-effort reset if mapper supports it.
		type resetter interface{ Reset() }
		if rs, ok := c.Mapper.(resetter); ok {
			rs.Reset()
		}
	}
	return nil
}

func applyOne(ctx context.Context, yamlDoc string, c Clients, o *Options) error {
	var obj map[string]any
	if err := yaml.Unmarshal([]byte(yamlDoc), &obj); err != nil {
		return fmt.Errorf("yaml unmarshal: %w", err)
	}
	u := &unstructured.Unstructured{Object: obj}

	gvk := u.GroupVersionKind()
	mapping, err := c.Mapper.RESTMapping(schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}, gvk.Version)
	if err != nil {
		return fmt.Errorf("RESTMapping for %s: %w", gvk.String(), err)
	}

	// Pick the correct ResourceInterface
	var ri dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if ns := u.GetNamespace(); ns == "" && o.DefaultNamespace != "" {
			u.SetNamespace(o.DefaultNamespace)
		}
		ri = c.Dynamic.Resource(mapping.Resource).Namespace(u.GetNamespace())
	} else {
		ri = c.Dynamic.Resource(mapping.Resource)
	}

	data, err := json.Marshal(u.Object)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}

	force := o.ForceConflicts
	po := metav1.PatchOptions{FieldManager: o.FieldManager, Force: &force}
	if o.DryRun {
		po.DryRun = []string{metav1.DryRunAll}
	}

	if _, err := ri.Patch(ctx, u.GetName(), types.ApplyPatchType, data, po); err != nil {
		return fmt.Errorf("apply %s %s/%s: %w", gvk.Kind, u.GetNamespace(), u.GetName(), err)
	}
	return nil
}
