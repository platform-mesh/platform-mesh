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

// Package bootstrap installs the Platform Mesh workspace tree into a kcp:
// :root:platform-mesh-system with the APIExports, the org/orgs/account
// WorkspaceTypes and :root:orgs.
//
// It is the single-binary counterpart of the platform-mesh-operator's
// KcpSetup subroutine, applying the same embedded manifests with Keycloak
// and OpenFGA switched off. Everything it does is idempotent.
package bootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"text/template"
	"time"

	kcpmanifests "go.platform-mesh.io/apis/static-assets/kcp"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	// SystemWorkspace is the workspace under :root holding the APIExports.
	SystemWorkspace = "platform-mesh-system"
	// OrgsWorkspace is the workspace under :root holding the orgs.
	OrgsWorkspace = "orgs"

	apisGroup = "apis.kcp.io"
	timeout   = 2 * time.Minute
	tick      = 250 * time.Millisecond
)

// workspaceTypeFiles are the WorkspaceTypes of :root, in creation order.
var workspaceTypeFiles = []string{
	"02-root/workspace-type-org.yaml",
	"02-root/01-workspace-type-orgs.yaml",
	"02-root/workspace-type-account.yaml",
}

// manifestData is what the manifest templates are rendered with. There is
// no Keycloak in the process, ever; Install fills in the rest.
var manifestData = map[string]any{
	"featureEnableTerminalControllerManager": false,
	"apiExportRootTenancyKcpIoIdentityHash":  "",
	"featureDisableKeycloak":                 true,
	"featureDisableOpenFGA":                  true,
}

// Options says what to install.
type Options struct {
	// Resources are the APIResourceSchemas and APIExports of the components
	// that run, applied into :root:platform-mesh-system.
	Resources []fs.FS
	// Bindings are the APIExports to bind in :root:platform-mesh-system
	// itself; kcp only serves an export's endpoint slice once it is bound.
	Bindings []string
	// Manifests are files of the embedded kcp manifests to apply in
	// :root:platform-mesh-system: their schemas, exports and endpoint slices
	// with Resources, the rest after the bindings.
	Manifests []string
	// PublicHost is the host:port browsers reach the portal at, without an
	// organization: the manifests of the micro frontends link to it.
	PublicHost string
	// OpenFGA renders the manifests with OpenFGA: organizations and
	// accounts then wait for the pm-openfga-operator.
	OpenFGA bool
	// WorkspaceTypes are manifest files of WorkspaceTypes for :root that
	// the org, orgs and account types may extend.
	WorkspaceTypes []string
	// OrgsManifests are manifest files to apply in :root:orgs.
	OrgsManifests []string
	// Objects are applied in :root:platform-mesh-system after the
	// bindings, replacing what an earlier run applied.
	Objects []*unstructured.Unstructured
	// AccountWebhook is where the account-operator's admission webhooks
	// serve; nil leaves Accounts without admission.
	AccountWebhook *Webhook
}

// Webhook is an admission webhook server of the process.
type Webhook struct {
	// URL is the server's base URL, such as https://127.0.0.1:9443.
	URL string
	// CAFile is the PEM CA of its serving certificate.
	CAFile string
}

// The manifests point at the account-operator's Service in a cluster.
const accountWebhookClusterURL = "https://account-operator-webhook.platform-mesh-system.svc.cluster.local:9443"

var accountWebhookFiles = map[string]string{
	"04-platform-mesh-system/mutatingwebhookconfiguration-admissionregistration.k8s.io.yaml":   "account-operator.webhooks.core.platform-mesh.io.ca-bundle",
	"04-platform-mesh-system/validatingwebhookconfiguration-admissionregistration.k8s.io.yaml": "organization-validator.webhooks.core.platform-mesh.io.ca-bundle",
}

// Platform is an installed workspace tree.
type Platform struct {
	Root, System, Orgs logicalcluster.Path
}

// Scheme returns a scheme with the kcp APIs bootstrap reads and writes.
func Scheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		kcpapisv1alpha1.AddToScheme,
		kcpapisv1alpha2.AddToScheme,
		kcpcorev1alpha1.AddToScheme,
		kcptenancyv1alpha1.AddToScheme,
	} {
		if err := add(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Install applies o to the kcp behind cfg, whose host must be the bare
// server URL, and waits until the bound exports are served.
func Install(ctx context.Context, cfg *rest.Config, o Options) (*Platform, error) {
	log := klog.FromContext(ctx).WithName("bootstrap")
	scheme, err := Scheme()
	if err != nil {
		return nil, err
	}
	cluster, err := clusterclient.New(cfg, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("building kcp cluster client: %w", err)
	}
	root := core.RootCluster.Path()
	p := &Platform{Root: root, System: root.Join(SystemWorkspace), Orgs: root.Join(OrgsWorkspace)}
	rootClient, systemClient, orgsClient := cluster.Cluster(p.Root), cluster.Cluster(p.System), cluster.Cluster(p.Orgs)

	log.Info("creating workspace", "path", p.System)
	if err := createWorkspace(ctx, rootClient, SystemWorkspace, nil); err != nil {
		return nil, err
	}

	ids, err := rootIdentities(ctx, rootClient)
	if err != nil {
		return nil, err
	}
	manifestData["featureDisableOpenFGA"] = !o.OpenFGA
	manifestData["baseDomainPort"] = o.PublicHost
	manifestData["apiExportRootTenancyKcpIoIdentityHash"] = ids["workspaces.tenancy.kcp.io"]

	var resources, manifests []*unstructured.Unstructured
	for _, file := range o.Manifests {
		objs, err := renderFile(file, nil)
		if err != nil {
			return nil, err
		}
		for _, obj := range objs {
			switch {
			case obj.GroupVersionKind().Group != apisGroup:
				manifests = append(manifests, obj)
			case obj.GetKind() == "APIResourceSchema", obj.GetKind() == "APIExport", obj.GetKind() == "APIExportEndpointSlice":
				resources = append(resources, obj)
			default:
				manifests = append(manifests, obj)
			}
		}
	}
	for _, fsys := range o.Resources {
		objs, err := readFS(fsys, "*.yaml")
		if err != nil {
			return nil, err
		}
		resources = append(resources, objs...)
	}
	for _, obj := range resources {
		ids.patch(obj)
	}
	log.Info("applying schemas and exports", "path", p.System, "objects", len(resources))
	if err := create(ctx, systemClient, sortForApply(resources)...); err != nil {
		return nil, err
	}

	served := map[string]bool{}
	for _, name := range o.Bindings {
		if err := bind(ctx, systemClient, systemClient, p.System, name); err != nil {
			return nil, err
		}
		served[name] = true
	}

	if err := create(ctx, systemClient, manifests...); err != nil {
		return nil, err
	}

	for _, obj := range o.Objects {
		if err := apply(ctx, systemClient, obj); err != nil {
			return nil, err
		}
	}

	if o.AccountWebhook != nil {
		log.Info("applying account admission webhooks", "path", p.System, "url", o.AccountWebhook.URL)
		if err := installAccountWebhooks(ctx, systemClient, o.AccountWebhook); err != nil {
			return nil, err
		}
	}

	log.Info("creating workspace types", "path", p.Root)
	for _, file := range o.WorkspaceTypes {
		objs, err := renderFile(file, nil)
		if err != nil {
			return nil, err
		}
		if err := create(ctx, rootClient, objs...); err != nil {
			return nil, err
		}
	}
	if err := installWorkspaceTypes(ctx, rootClient, systemClient, p); err != nil {
		return nil, err
	}

	log.Info("creating workspace", "path", p.Orgs)
	if err := createWorkspace(ctx, rootClient, OrgsWorkspace, &kcptenancyv1alpha1.WorkspaceTypeReference{Name: "orgs", Path: root.String()}); err != nil {
		return nil, err
	}
	names, err := orgsBindings()
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if ok, err := exists(ctx, systemClient, &kcpapisv1alpha2.APIExport{}, name); err != nil {
			return nil, err
		} else if !ok {
			log.V(2).Info("not binding export, not installed", "path", p.Orgs, "export", name)
			continue
		}
		if err := bind(ctx, systemClient, orgsClient, p.System, name); err != nil {
			return nil, err
		}
		served[name] = true
	}

	for _, file := range o.OrgsManifests {
		objs, err := renderFile(file, nil)
		if err != nil {
			return nil, err
		}
		if err := create(ctx, orgsClient, objs...); err != nil {
			return nil, err
		}
	}

	for name := range served {
		if err := waitEndpointSlice(ctx, systemClient, name); err != nil {
			return nil, err
		}
	}
	log.Info("platform mesh workspaces are ready", "system", p.System, "orgs", p.Orgs)
	return p, nil
}

func poll(ctx context.Context, what string, cond func(ctx context.Context) error) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, tick, timeout, true, func(ctx context.Context) (bool, error) {
		lastErr = cond(ctx)
		return lastErr == nil, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for %s: %w (last: %v)", what, err, lastErr)
	}
	return nil
}

func exists(ctx context.Context, cl ctrlruntimeclient.Client, obj ctrlruntimeclient.Object, name string) (bool, error) {
	err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, obj)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// createWorkspace creates the workspace name in parent's workspace and waits
// until it is Ready.
func createWorkspace(ctx context.Context, parent ctrlruntimeclient.Client, name string, wsType *kcptenancyv1alpha1.WorkspaceTypeReference) error {
	ws := &kcptenancyv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       kcptenancyv1alpha1.WorkspaceSpec{Type: wsType},
	}
	if err := parent.Create(ctx, ws); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating workspace %s: %w", name, err)
	}
	return poll(ctx, "workspace "+name, func(ctx context.Context) error {
		got := &kcptenancyv1alpha1.Workspace{}
		if err := parent.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, got); err != nil {
			return err
		}
		if got.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
			return fmt.Errorf("phase %q", got.Status.Phase)
		}
		return nil
	})
}

// bind binds the export name of the workspace at exportPath in into's
// workspace, accepting its permission claims, and waits until it is Bound.
func bind(ctx context.Context, exports, into ctrlruntimeclient.Client, exportPath logicalcluster.Path, name string) error {
	export := &kcpapisv1alpha2.APIExport{}
	if err := exports.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, export); err != nil {
		return fmt.Errorf("getting APIExport %s: %w", name, err)
	}
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
	binding := &kcpapisv1alpha2.APIBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kcpapisv1alpha2.APIBindingSpec{
			Reference: kcpapisv1alpha2.BindingReference{
				Export: &kcpapisv1alpha2.ExportBindingReference{Path: exportPath.String(), Name: name},
			},
			PermissionClaims: claims,
		},
	}
	if err := into.Create(ctx, binding); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating APIBinding %s: %w", name, err)
	}
	return poll(ctx, "APIBinding "+name, func(ctx context.Context) error {
		got := &kcpapisv1alpha2.APIBinding{}
		if err := into.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, got); err != nil {
			return err
		}
		if got.Status.Phase != kcpapisv1alpha2.APIBindingPhaseBound {
			return fmt.Errorf("phase %q", got.Status.Phase)
		}
		return nil
	})
}

func waitEndpointSlice(ctx context.Context, cl ctrlruntimeclient.Client, name string) error {
	return poll(ctx, "APIExportEndpointSlice "+name, func(ctx context.Context) error {
		slice := &kcpapisv1alpha1.APIExportEndpointSlice{}
		if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name}, slice); err != nil {
			return err
		}
		if len(slice.Status.APIExportEndpoints) == 0 || slice.Status.APIExportEndpoints[0].URL == "" {
			return errors.New("no endpoint URL")
		}
		return nil
	})
}

// installAccountWebhooks applies the account webhook configurations of the
// manifests with the server and CA of the process, replacing what an
// earlier run applied.
func installAccountWebhooks(ctx context.Context, systemClient ctrlruntimeclient.Client, w *Webhook) error {
	ca, err := os.ReadFile(w.CAFile)
	if err != nil {
		return err
	}
	for file, caKey := range accountWebhookFiles {
		objs, err := renderFile(file, map[string]any{caKey: base64.StdEncoding.EncodeToString(ca)})
		if err != nil {
			return err
		}
		for _, obj := range objs {
			hooks, _, _ := unstructured.NestedSlice(obj.Object, "webhooks")
			for _, h := range hooks {
				hook, _ := h.(map[string]any)
				url, _, _ := unstructured.NestedString(hook, "clientConfig", "url")
				if !strings.HasPrefix(url, accountWebhookClusterURL) {
					return fmt.Errorf("%s: unexpected webhook url %q", file, url)
				}
				if err := unstructured.SetNestedField(hook, w.URL+strings.TrimPrefix(url, accountWebhookClusterURL), "clientConfig", "url"); err != nil {
					return err
				}
			}
			if err := unstructured.SetNestedSlice(obj.Object, hooks, "webhooks"); err != nil {
				return err
			}
			if err := apply(ctx, systemClient, obj); err != nil {
				return err
			}
		}
	}
	return nil
}

// apply creates obj or, when it exists, replaces it. It retries, as create
// does.
func apply(ctx context.Context, cl ctrlruntimeclient.Client, obj *unstructured.Unstructured) error {
	return poll(ctx, obj.GetKind()+" "+obj.GetName(), func(ctx context.Context) error {
		err := cl.Create(ctx, obj.DeepCopy())
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(obj.GroupVersionKind())
		if err := cl.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(obj), existing); err != nil {
			return err
		}
		desired := obj.DeepCopy()
		desired.SetResourceVersion(existing.GetResourceVersion())
		desired.SetFinalizers(existing.GetFinalizers())
		return cl.Update(ctx, desired)
	})
}

// installWorkspaceTypes creates workspaceTypeFiles in :root, dropping default
// bindings and extends that refer to things that are not installed: their
// workspaces would never initialise.
func installWorkspaceTypes(ctx context.Context, rootClient, systemClient ctrlruntimeclient.Client, p *Platform) error {
	for _, file := range workspaceTypeFiles {
		objs, err := renderFile(file, nil)
		if err != nil {
			return err
		}
		for _, wst := range objs {
			bindings, _, _ := unstructured.NestedSlice(wst.Object, "spec", "defaultAPIBindings")
			kept := make([]any, 0, len(bindings))
			for _, b := range bindings {
				ref, _ := b.(map[string]any)
				if ref["path"] == p.System.String() {
					ok, err := exists(ctx, systemClient, &kcpapisv1alpha2.APIExport{}, fmt.Sprint(ref["export"]))
					if err != nil {
						return err
					}
					if !ok {
						continue
					}
				}
				kept = append(kept, b)
			}
			if err := unstructured.SetNestedSlice(wst.Object, kept, "spec", "defaultAPIBindings"); err != nil {
				return err
			}

			extends, _, _ := unstructured.NestedSlice(wst.Object, "spec", "extend", "with")
			keptExtends := make([]any, 0, len(extends))
			for _, e := range extends {
				ref, _ := e.(map[string]any)
				if ref["path"] == core.RootCluster.String() {
					ok, err := exists(ctx, rootClient, &kcptenancyv1alpha1.WorkspaceType{}, fmt.Sprint(ref["name"]))
					if err != nil {
						return err
					}
					if !ok {
						continue
					}
				}
				keptExtends = append(keptExtends, e)
			}
			if len(keptExtends) == 0 {
				unstructured.RemoveNestedField(wst.Object, "spec", "extend")
			} else if err := unstructured.SetNestedSlice(wst.Object, keptExtends, "spec", "extend", "with"); err != nil {
				return err
			}

			// The bind permission check can lag behind a new APIExport.
			if err := poll(ctx, "WorkspaceType "+wst.GetName(), func(ctx context.Context) error {
				err := rootClient.Create(ctx, wst.DeepCopy())
				if apierrors.IsAlreadyExists(err) {
					return nil
				}
				return err
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// orgsBindings returns the platform-mesh-system exports the manifests bind
// in :root:orgs.
func orgsBindings() ([]string, error) {
	files, err := fs.Glob(kcpmanifests.Resources, "03-orgs/apibinding-*.yaml")
	if err != nil {
		return nil, err
	}
	var exports []string
	for _, file := range files {
		objs, err := renderFile(file, nil)
		if err != nil {
			return nil, err
		}
		for _, binding := range objs {
			export, _, _ := unstructured.NestedString(binding.Object, "spec", "reference", "export", "name")
			if export == "" {
				return nil, fmt.Errorf("%s: no bound export", file)
			}
			exports = append(exports, export)
		}
	}
	return exports, nil
}

// renderFile renders a manifest template with manifestData and extra.
func renderFile(file string, extra map[string]any) ([]*unstructured.Unstructured, error) {
	raw, err := fs.ReadFile(kcpmanifests.Resources, file)
	if err != nil {
		return nil, err
	}
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
	tmpl, err := template.New(file).Funcs(funcs).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", file, err)
	}
	var out bytes.Buffer
	data := maps.Clone(manifestData)
	maps.Copy(data, extra)
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("rendering %s: %w", file, err)
	}
	// The manifests link to the micro frontends with https, as behind a
	// cluster's gateway; the process serves plain http on localhost.
	rendered := bytes.ReplaceAll(out.Bytes(), []byte("https://{context.organization}."), []byte("http://{context.organization}."))
	return decode(rendered)
}

func readFS(fsys fs.FS, glob string) ([]*unstructured.Unstructured, error) {
	files, err := fs.Glob(fsys, glob)
	if err != nil {
		return nil, err
	}
	var objs []*unstructured.Unstructured
	for _, file := range files {
		raw, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, err
		}
		decoded, err := decode(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		objs = append(objs, decoded...)
	}
	return objs, nil
}

func decode(raw []byte) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
	for {
		obj := map[string]any{}
		err := decoder.Decode(&obj)
		if errors.Is(err, io.EOF) {
			return objs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decoding YAML: %w", err)
		}
		if len(obj) == 0 {
			continue
		}
		objs = append(objs, &unstructured.Unstructured{Object: obj})
	}
}

// create creates objs in order, leaving what exists as it is. It retries:
// right after a restart kcp serves a workspace before its discovery.
func create(ctx context.Context, cl ctrlruntimeclient.Client, objs ...*unstructured.Unstructured) error {
	for _, obj := range objs {
		if err := poll(ctx, obj.GetKind()+" "+obj.GetName(), func(ctx context.Context) error {
			err := cl.Create(ctx, obj.DeepCopy())
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// sortForApply orders APIResourceSchemas before APIExports before the rest.
func sortForApply(objs []*unstructured.Unstructured) []*unstructured.Unstructured {
	rank := func(obj *unstructured.Unstructured) int {
		if obj.GroupVersionKind().Group != apisGroup {
			return 2
		}
		switch obj.GetKind() {
		case "APIResourceSchema":
			return 0
		case "APIExport":
			return 1
		default:
			return 2
		}
	}
	sorted := slices.Clone(objs)
	slices.SortStableFunc(sorted, func(a, b *unstructured.Unstructured) int { return rank(a) - rank(b) })
	return sorted
}

// identities maps the resources of kcp's own exports to their identity hash,
// keyed by "<resource>.<group>".
type identities map[string]string

// patch fills the identityHash of an APIExport's permission claims on
// resources ids knows and defaults missing verbs to "*".
func (ids identities) patch(obj *unstructured.Unstructured) {
	if obj.GroupVersionKind().Group != apisGroup || obj.GetKind() != "APIExport" {
		return
	}
	claims, found, err := unstructured.NestedSlice(obj.Object, "spec", "permissionClaims")
	if !found || err != nil {
		return
	}
	for i := range claims {
		claim, ok := claims[i].(map[string]any)
		if !ok {
			continue
		}
		group, _ := claim["group"].(string)
		resource, _ := claim["resource"].(string)
		if hash, ok := ids[resource+"."+group]; ok {
			claim["identityHash"] = hash
		}
		// apigen writes `verbs: null`, which kcp rejects.
		if claim["verbs"] == nil {
			claim["verbs"] = []any{"*"}
		}
	}
	_ = unstructured.SetNestedSlice(obj.Object, claims, "spec", "permissionClaims")
}

// rootIdentities reads the identity hashes of the APIExports in :root.
func rootIdentities(ctx context.Context, root ctrlruntimeclient.Client) (identities, error) {
	exports := &kcpapisv1alpha2.APIExportList{}
	if err := root.List(ctx, exports); err != nil {
		return nil, fmt.Errorf("listing APIExports in root: %w", err)
	}
	ids := identities{}
	for _, export := range exports.Items {
		if export.Status.IdentityHash == "" {
			continue
		}
		for _, r := range export.Spec.Resources {
			ids[r.Name+"."+r.Group] = export.Status.IdentityHash
		}
	}
	return ids, nil
}
