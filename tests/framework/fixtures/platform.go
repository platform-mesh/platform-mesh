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
	"go.platform-mesh.io/tests/framework/kcp"
	"go.platform-mesh.io/tests/framework/scheme"
	"go.platform-mesh.io/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

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

// workspaceTypeFiles are the WorkspaceTypes of :root the suites need, in
// creation order, as the platform-mesh-operator deploys them.
var workspaceTypeFiles = []string{
	"02-root/workspace-type-org.yaml",
	"02-root/01-workspace-type-orgs.yaml",
	"02-root/workspace-type-account.yaml",
}

// manifestData is what the WorkspaceType templates are rendered with: no
// optional feature is enabled.
var manifestData = map[string]any{
	"featureEnableTerminalControllerManager": "false",
}

// Platform is a platform-mesh-system tree in a kcp, what the account,
// security and extension-manager suites each built by hand.
type Platform struct {
	// Root, System and Orgs are :root, :root:platform-mesh-system and
	// :root:orgs. Orgs is empty unless WithOrgs.
	Root, System, Orgs logicalcluster.Path
	Cluster            clusterclient.ClusterClient
	// OrgsClient is nil unless WithOrgs.
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
// :root:platform-mesh-system, e.g. WithResources(accountresources.Resources,
// "resources/*.yaml"). Repeatable; an object that already exists is kept.
func WithResources(fsys fs.FS, glob string) Option {
	return func(o *options) { o.resources = append(o.resources, resourceGlob{fsys: fsys, glob: glob}) }
}

// WithExport creates the APIExport name in :root:platform-mesh-system serving
// the named resources of the APIExport from (one of WithResources), which
// stops serving them, and sharing from's identity. This is how the deployed
// manifests (apis/static-assets/kcp) split stores and
// authorizationmodels out of core.platform-mesh.io into fga.platform-mesh.io
// and provider-fga.platform-mesh.io, while apigen puts every resource of the
// group into one export.
func WithExport(name, from string, resources ...string) Option {
	return func(o *options) {
		o.exports = append(o.exports, exportSplit{name: name, from: from, resources: resources})
	}
}

// WithBindings binds the named APIExports of :root:platform-mesh-system in
// that same workspace, accepting every permission claim they make (see
// BindingFor), and waits for the bindings to be Bound.
func WithBindings(exports ...string) Option {
	return func(o *options) { o.bindings = append(o.bindings, exports...) }
}

// WithWorkspaceTypes creates the org, orgs and account WorkspaceTypes in
// :root from the platform-mesh-operator's manifests (see
// installWorkspaceTypes for what is dropped when it is not installed).
func WithWorkspaceTypes() Option {
	return func(o *options) { o.workspaceTypes = true }
}

// WithDefaultBindings adds default bindings to the named exports of
// :root:platform-mesh-system to the WorkspaceType typeName, on top of its
// manifest; for what a workspace gets from an operator that does not run in
// the suite. It implies WithWorkspaceTypes.
func WithDefaultBindings(typeName string, exports ...string) Option {
	return func(o *options) {
		o.workspaceTypes = true
		if o.defaultBindings == nil {
			o.defaultBindings = map[string][]string{}
		}
		o.defaultBindings[typeName] = append(o.defaultBindings[typeName], exports...)
	}
}

// WithOrgs creates :root:orgs of type root:orgs and waits for it to be
// Ready. It implies WithWorkspaceTypes.
func WithOrgs() Option {
	return func(o *options) { o.workspaceTypes, o.orgs = true, true }
}

// WithScheme sets the scheme of the returned clients. It must contain what
// scheme.New registers; the default is scheme.New(t).
func WithScheme(s *runtime.Scheme) Option {
	return func(o *options) { o.scheme = s }
}

// Install creates :root:platform-mesh-system (fixed name, an existing one is
// reused) and applies the options in this order: APIResourceSchemas,
// APIExports (after WithExport splits), APIBindings (waited for Bound), the
// remaining resources, WorkspaceTypes, :root:orgs.
//
// Permission claims of APIExports and APIBindings on resources served by a
// kcp export in :root (tenancy.kcp.io, topology.kcp.io, ...) get that
// export's status.identityHash. WorkspaceType creation is retried for 30s,
// since the bind permission check can lag behind a new APIExport on another
// shard. Finally Install waits until the APIExportEndpointSlice of every
// export something was bound to (WithBindings, and the orgs workspace's
// default bindings) has an endpoint URL; kcp lists no endpoints for an
// export nothing is bound to.
func Install(t testing.TB, env *kcp.Env, opts ...Option) *Platform {
	t.Helper()
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	if o.scheme == nil {
		o.scheme = scheme.New(t)
	}

	root := core.RootCluster.Path()
	p := &Platform{Root: root, System: root.Join(SystemWorkspace), Cluster: env.ClusterClient(t, o.scheme)}
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
		// Read the export back rather than using the manifest: an existing
		// export is kept as it is, and its claims carry the identity hashes.
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

// BindingFor returns the APIBinding a workspace creates to bind export by
// path: named after the export, accepting every permission claim it makes
// with the claim's defaultSelector or matchAll, the way kcp does for a
// WorkspaceType's defaultAPIBindings. This is the binding the deployed
// manifests (apis/static-assets/kcp) write out by hand.
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

// splitExport moves the resources named in s out of the APIExport s.from in
// objs into a new APIExport s.name that shares s.from's identity secret
// (kcp-system/<from>, the secret kcp creates for an export without
// spec.identity), and returns objs with the new export appended.
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

// Render executes raw as a Go text/template with data, the way the
// platform-mesh-operator renders its manifests (subroutines.ReplaceTemplate);
// a key the template needs but data lacks is an error.
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

// installWorkspaceTypes creates the WorkspaceTypes of workspaceTypeFiles in
// :root as the platform-mesh-operator deploys them, plus extra default
// bindings (WithDefaultBindings). Two things are dropped when not installed,
// otherwise workspaces of the type would never finish initialising: a default
// binding to an export in :root:platform-mesh-system that does not exist
// there, and an extended WorkspaceType in :root that does not exist (the
// deployed org and account types extend `security`, whose initializer the
// security-operator clears).
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

// systemDefaultBindings returns the exports in :root:platform-mesh-system the
// WorkspaceType name in :root binds by default.
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

// ensureWorkspace creates the workspace name of type root:<typeName> under
// parent unless it exists, and waits until it is Ready and reachable by path.
// Unlike envtest.NewWorkspaceFixture it uses a fixed name and never deletes
// the workspace, so packages sharing a kcp share it.
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
