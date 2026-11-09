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

package fixtures

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/require"

	kcpmanifests "go.platform-mesh.io/apis/static-assets/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/logicalcluster/v3"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	// SystemWorkspace is the workspace under :root holding the platform-mesh
	// APIExports, as in a real installation.
	SystemWorkspace = "platform-mesh-system"
	// OrgsWorkspace is the workspace under :root holding the orgs.
	OrgsWorkspace = "orgs"

	workspaceTypeTimeout = 30 * time.Second
)

// workspaceTypeFiles are the WorkspaceTypes of :root, in creation order.
var workspaceTypeFiles = []string{
	"02-root/workspace-type-org.yaml",
	"02-root/01-workspace-type-orgs.yaml",
	"02-root/workspace-type-account.yaml",
}

// manifestData renders the WorkspaceType templates with every feature off.
var manifestData = map[string]any{
	"featureEnableTerminalControllerManager": "false",
}

// Platform is a platform-mesh-system tree in a kcp.
type Platform struct {
	// Orgs and OrgsClient are set only with WithOrgs.
	Root, System, Orgs                   logicalcluster.Path
	Cluster                              clusterclient.ClusterClient
	RootClient, SystemClient, OrgsClient ctrlruntimeclient.Client
}

// Option configures Install.
type Option func(*options)

type options struct {
	resources       []resourceGlob
	exports         []exportSplit
	bindings        []string
	workspaceTypes  bool
	defaultBindings map[string][]string
	orgs            bool
	scheme          *runtime.Scheme
}

type resourceGlob struct {
	fsys fs.FS
	glob string
}

type exportSplit struct {
	name, from string
	resources  []string
}

// WithResources applies the files in fsys matching glob into
// :root:platform-mesh-system. Existing objects are kept.
func WithResources(fsys fs.FS, glob string) Option {
	return func(o *options) { o.resources = append(o.resources, resourceGlob{fsys: fsys, glob: glob}) }
}

// WithExport moves the named resources of the APIExport from into a new
// APIExport name sharing from's identity, the way the deployed manifests
// split fga.platform-mesh.io out of core.platform-mesh.io.
func WithExport(name, from string, resources ...string) Option {
	return func(o *options) {
		o.exports = append(o.exports, exportSplit{name: name, from: from, resources: resources})
	}
}

// WithBindings binds the named APIExports in :root:platform-mesh-system
// (see BindingFor) and waits for them to be Bound.
func WithBindings(exports ...string) Option {
	return func(o *options) { o.bindings = append(o.bindings, exports...) }
}

// WithWorkspaceTypes creates the org, orgs and account WorkspaceTypes in :root.
func WithWorkspaceTypes() Option {
	return func(o *options) { o.workspaceTypes = true }
}

// WithDefaultBindings adds default bindings to the named exports to the
// WorkspaceType typeName. It implies WithWorkspaceTypes.
func WithDefaultBindings(typeName string, exports ...string) Option {
	return func(o *options) {
		o.workspaceTypes = true
		if o.defaultBindings == nil {
			o.defaultBindings = map[string][]string{}
		}
		o.defaultBindings[typeName] = append(o.defaultBindings[typeName], exports...)
	}
}

// WithOrgs creates :root:orgs. It implies WithWorkspaceTypes.
func WithOrgs() Option {
	return func(o *options) { o.workspaceTypes, o.orgs = true, true }
}

// WithScheme sets the scheme of the returned clients (default scheme.New).
func WithScheme(s *runtime.Scheme) Option {
	return func(o *options) { o.scheme = s }
}

// Install creates :root:platform-mesh-system (reusing an existing one) and
// applies the options: schemas, exports, bindings, other resources,
// WorkspaceTypes, :root:orgs. It waits until every bound export's
// APIExportEndpointSlice has a URL.
func Install(t testing.TB, instance *kcptc.Instance, opts ...Option) *Platform {
	t.Helper()
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	if o.scheme == nil {
		o.scheme = scheme.New(t)
	}

	cluster, err := clusterclient.New(kcp.Config(instance), ctrlruntimeclient.Options{Scheme: o.scheme})
	require.NoError(t, err, "building kcp cluster client")
	root := core.RootCluster.Path()
	p := &Platform{Root: root, System: root.Join(SystemWorkspace), Cluster: cluster}
	p.RootClient = p.Cluster.Cluster(root)
	ensureWorkspace(t, p.Cluster, root, SystemWorkspace, "universal")
	p.SystemClient = p.Cluster.Cluster(p.System)

	ids := rootIdentities(t, p.RootClient)
	var resources []*unstructured.Unstructured
	for _, r := range o.resources {
		resources = append(resources, readFS(t, r.fsys, r.glob)...)
	}
	for _, obj := range resources {
		ids.patch(obj)
	}
	for _, split := range o.exports {
		resources = splitExport(t, resources, split)
	}
	resources = sortForApply(resources)
	// Schemas and exports first, the rest after the bindings it may need.
	split := 0
	for split < len(resources) && resources[split].GroupVersionKind().Group == apisGroup &&
		(resources[split].GetKind() == "APIResourceSchema" || resources[split].GetKind() == "APIExport") {
		split++
	}
	Apply(t, p.SystemClient, toObjects(resources[:split])...)

	served := map[string]bool{} // exports in platform-mesh-system something binds to
	for _, name := range o.bindings {
		// The export in kcp, not the manifest: existing ones are kept as is.
		export := &kcpapisv1alpha2.APIExport{}
		require.NoError(t, p.SystemClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, export), "getting APIExport %s", name)
		Apply(t, p.SystemClient, BindingFor(export, p.System))
		wait.APIBindingBound(t, p.SystemClient, name)
		served[name] = true
	}
	Apply(t, p.SystemClient, toObjects(resources[split:])...)

	if o.workspaceTypes {
		installWorkspaceTypes(t, p, o.defaultBindings)
	}
	if o.orgs {
		ensureWorkspace(t, p.Cluster, root, OrgsWorkspace, "orgs")
		p.Orgs = root.Join(OrgsWorkspace)
		p.OrgsClient = p.Cluster.Cluster(p.Orgs)
		for _, name := range systemDefaultBindings(t, p, "orgs") {
			served[name] = true
		}
	}

	for name := range served {
		wait.EndpointSliceServed(t, p.SystemClient, name)
	}
	return p
}

// BindingFor returns an APIBinding to export, accepting every permission
// claim it makes (defaultSelector or matchAll).
func BindingFor(export *kcpapisv1alpha2.APIExport, path logicalcluster.Path) *kcpapisv1alpha2.APIBinding {
	claims := make([]kcpapisv1alpha2.AcceptablePermissionClaim, 0, len(export.Spec.PermissionClaims))
	for _, claim := range export.Spec.PermissionClaims {
		selector := kcpapisv1alpha2.PermissionClaimSelector{MatchAll: true}
		if claim.DefaultSelector != nil {
			selector = *claim.DefaultSelector
		}
		claims = append(claims, kcpapisv1alpha2.AcceptablePermissionClaim{
			ScopedPermissionClaim: kcpapisv1alpha2.ScopedPermissionClaim{
				PermissionClaim: kcpapisv1alpha2.PermissionClaim{
					GroupResource: claim.GroupResource,
					Verbs:         claim.Verbs,
					IdentityHash:  claim.IdentityHash,
				},
				Selector: selector,
			},
			State: kcpapisv1alpha2.ClaimAccepted,
		})
	}
	return &kcpapisv1alpha2.APIBinding{
		ObjectMeta: metav1.ObjectMeta{Name: export.Name},
		Spec: kcpapisv1alpha2.APIBindingSpec{
			Reference: kcpapisv1alpha2.BindingReference{
				Export: &kcpapisv1alpha2.ExportBindingReference{Path: path.String(), Name: export.Name},
			},
			PermissionClaims: claims,
		},
	}
}

// splitExport moves s.resources from APIExport s.from into a new APIExport
// s.name sharing s.from's identity secret (kcp-system/<from>).
func splitExport(t testing.TB, objs []*unstructured.Unstructured, s exportSplit) []*unstructured.Unstructured {
	t.Helper()
	var from *unstructured.Unstructured
	for _, obj := range objs {
		if obj.GroupVersionKind().Group == apisGroup && obj.GetKind() == "APIExport" && obj.GetName() == s.from {
			from = obj
			break
		}
	}
	require.NotNil(t, from, "WithExport(%s): APIExport %s is not among the resources", s.name, s.from)

	all, _, err := unstructured.NestedSlice(from.Object, "spec", "resources")
	require.NoError(t, err, "WithExport(%s): reading spec.resources of APIExport %s", s.name, s.from)
	want := map[string]bool{}
	for _, name := range s.resources {
		want[name] = true
	}
	var kept, moved []any
	for _, r := range all {
		if m, ok := r.(map[string]any); ok && want[fmt.Sprint(m["name"])] {
			delete(want, fmt.Sprint(m["name"]))
			moved = append(moved, r)
			continue
		}
		kept = append(kept, r)
	}
	require.Empty(t, want, "WithExport(%s): APIExport %s does not serve these resources", s.name, s.from)
	require.NoError(t, unstructured.SetNestedSlice(from.Object, kept, "spec", "resources"))

	export := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apisGroup + "/v1alpha2",
		"kind":       "APIExport",
		"metadata":   map[string]any{"name": s.name},
		"spec": map[string]any{
			"identity":  map[string]any{"secretRef": map[string]any{"namespace": "kcp-system", "name": s.from}},
			"resources": moved,
		},
	}}
	return append(objs, export)
}

// Render executes raw as a text/template with data; missing keys are errors.
func Render(t testing.TB, raw []byte, data map[string]any) []byte {
	t.Helper()
	funcs := template.FuncMap{
		"indent": func(spaces int, s string) string {
			pad := strings.Repeat(" ", spaces)
			lines := strings.Split(s, "\n")
			for i, line := range lines {
				if line != "" {
					lines[i] = pad + line
				}
			}
			return strings.Join(lines, "\n")
		},
	}
	tmpl, err := template.New("manifest").Funcs(funcs).Option("missingkey=error").Parse(string(raw))
	require.NoError(t, err, "parsing manifest template")
	var out bytes.Buffer
	require.NoError(t, tmpl.Execute(&out, data), "rendering manifest template")
	return out.Bytes()
}

// installWorkspaceTypes creates workspaceTypeFiles in :root plus extra
// default bindings, dropping default bindings and extends that refer to
// things not installed (their workspaces would never initialise).
func installWorkspaceTypes(t testing.TB, p *Platform, extra map[string][]string) {
	t.Helper()
	for _, file := range workspaceTypeFiles {
		raw, err := fs.ReadFile(kcpmanifests.Resources, file)
		require.NoError(t, err)
		for _, wst := range Decode(t, Render(t, raw, manifestData)) {
			bindings, _, _ := unstructured.NestedSlice(wst.Object, "spec", "defaultAPIBindings")
			for _, export := range extra[wst.GetName()] {
				bindings = append(bindings, map[string]any{"export": export, "path": p.System.String()})
			}
			kept := make([]any, 0, len(bindings))
			for _, b := range bindings {
				ref, _ := b.(map[string]any)
				if ref["path"] == p.System.String() && !exportExists(t, p.SystemClient, fmt.Sprint(ref["export"])) {
					t.Logf("WorkspaceType %s: dropping default binding to %s:%s, not installed", wst.GetName(), ref["path"], ref["export"])
					continue
				}
				kept = append(kept, b)
			}
			require.NoError(t, unstructured.SetNestedSlice(wst.Object, kept, "spec", "defaultAPIBindings"))

			extends, _, _ := unstructured.NestedSlice(wst.Object, "spec", "extend", "with")
			keptExtends := make([]any, 0, len(extends))
			for _, e := range extends {
				ref, _ := e.(map[string]any)
				if ref["path"] == core.RootCluster.String() && !workspaceTypeExists(t, p.RootClient, fmt.Sprint(ref["name"])) {
					t.Logf("WorkspaceType %s: dropping extend of %s:%s, not installed", wst.GetName(), ref["path"], ref["name"])
					continue
				}
				keptExtends = append(keptExtends, e)
			}
			if len(keptExtends) == 0 {
				unstructured.RemoveNestedField(wst.Object, "spec", "extend")
			} else {
				require.NoError(t, unstructured.SetNestedSlice(wst.Object, keptExtends, "spec", "extend", "with"))
			}

			// The bind permission check can lag behind a new APIExport on another shard.
			wait.Eventually(t, func() (bool, string) {
				err := p.RootClient.Create(t.Context(), wst.DeepCopy())
				if err == nil || apierrors.IsAlreadyExists(err) {
					return true, ""
				}
				return false, err.Error()
			}, workspaceTypeTimeout, 500*time.Millisecond, "WorkspaceType %s should be created", wst.GetName())
		}
	}
}

// systemDefaultBindings returns the platform-mesh-system exports WorkspaceType name binds by default.
func systemDefaultBindings(t testing.TB, p *Platform, name string) []string {
	t.Helper()
	wst := &kcptenancyv1alpha1.WorkspaceType{}
	require.NoError(t, p.RootClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, wst))
	var exports []string
	for _, b := range wst.Spec.DefaultAPIBindings {
		if b.Path == p.System.String() {
			exports = append(exports, b.Export)
		}
	}
	return exports
}

func workspaceTypeExists(t testing.TB, root ctrlruntimeclient.Client, name string) bool {
	t.Helper()
	err := root.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, &kcptenancyv1alpha1.WorkspaceType{})
	if apierrors.IsNotFound(err) {
		return false
	}
	require.NoError(t, err, "getting WorkspaceType %s", name)
	return true
}

func exportExists(t testing.TB, cl ctrlruntimeclient.Client, name string) bool {
	t.Helper()
	export := &unstructured.Unstructured{}
	export.SetAPIVersion(apisGroup + "/v1alpha2")
	export.SetKind("APIExport")
	err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, export)
	if apierrors.IsNotFound(err) {
		return false
	}
	require.NoError(t, err, "getting APIExport %s", name)
	return true
}

// ensureWorkspace creates workspace name of type root:<typeName> under parent
// unless it exists and waits until it is Ready and reachable by path.
func ensureWorkspace(t testing.TB, cluster clusterclient.ClusterClient, parent logicalcluster.Path, name, typeName string) {
	t.Helper()
	parentClient := cluster.Cluster(parent)
	ws := &kcptenancyv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kcptenancyv1alpha1.WorkspaceSpec{
			Type: &kcptenancyv1alpha1.WorkspaceTypeReference{
				Name: kcptenancyv1alpha1.WorkspaceTypeName(typeName),
				Path: core.RootCluster.String(),
			},
		},
	}
	// A WorkspaceType created a moment ago may not be known to admission yet.
	wait.Eventually(t, func() (bool, string) {
		err := parentClient.Create(t.Context(), ws.DeepCopy())
		if err == nil || apierrors.IsAlreadyExists(err) {
			return true, ""
		}
		return false, fmt.Sprintf("creating workspace: %v", err)
	}, wait.Timeout, wait.Tick, "workspace %s should be created", parent.Join(name))
	wait.WorkspaceReady(t, parentClient, name)

	path := parent.Join(name)
	wait.Eventually(t, func() (bool, string) {
		lc := &kcpcorev1alpha1.LogicalCluster{}
		if err := cluster.Cluster(path).Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: kcpcorev1alpha1.LogicalClusterName}, lc); err != nil {
			return false, fmt.Sprintf("getting LogicalCluster: %v", err)
		}
		return true, ""
	}, wait.Timeout, wait.Tick, "workspace %s should be reachable", path)
}
