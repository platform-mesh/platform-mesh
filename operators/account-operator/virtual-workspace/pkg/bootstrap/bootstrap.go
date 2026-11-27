/*
Copyright 2026 The kcp Authors.

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

// Package bootstrap installs the kcp-side objects the access virtual workspace
// needs and verifies they came up.
package bootstrap

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	configapiexport "go.platform-mesh.io/account-operator/virtual-workspace/config/apiexport"
	configcontroller "go.platform-mesh.io/account-operator/virtual-workspace/config/controller"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

// APIExportName is the APIExport, and the APIExportEndpointSlice, this
// component installs and serves.
const APIExportName = "access.platform-mesh.io"

const (
	// ControllerNamespace is the namespace inside the APIExport's
	// workspace holding the server's identity and generated kubeconfig.
	ControllerNamespace = "default"

	// ControllerServiceAccount is the identity the server runs as. It lives
	// in the same workspace as the APIExport so the server never needs an
	// admin kubeconfig.
	ControllerServiceAccount = "access-vw-controller"

	// ControllerTokenSecret is the ServiceAccount token secret kcp
	// populates.
	ControllerTokenSecret = "access-vw-controller-token"

	// ControllerKubeconfigSecret holds the kubeconfig built from that
	// token, for the server to mount.
	ControllerKubeconfigSecret = "access-vw-kubeconfig"

	// ControllerClusterRole is the role carrying everything the server needs
	// in this workspace, including the verb=access rule that gates the rest.
	ControllerClusterRole = "access-vw-controller"

	// ServerIdentityBinding binds ControllerClusterRole to identities named by
	// Options.ServerUsers/ServerGroups. Separate from the committed binding
	// because those names are deployment-specific.
	ServerIdentityBinding = "access-vw-controller-server"
)

var apiExportEndpointSliceGVR = schema.GroupVersionResource{
	Group: "apis.kcp.io", Version: "v1alpha1", Resource: "apiexportendpointslices",
}

var logicalClusterGVR = schema.GroupVersionResource{
	Group: "core.kcp.io", Version: "v1alpha1", Resource: "logicalclusters",
}

var createOnlyKinds = map[string]bool{"APIResourceSchema": true}

// Result reports what a successful bootstrap produced, so callers can
// print the values the server needs.
type Result struct {
	// WorkspacePath is the absolute path of the workspace the objects
	// were installed into.
	WorkspacePath string

	// APIExportEndpointSlice is the name to pass to the server's
	// --apiexport-endpointslice flag.
	APIExportEndpointSlice string

	// VirtualWorkspaceURLs are the per-shard URLs the slice resolved to.
	// Empty means the provider will discover nothing.
	VirtualWorkspaceURLs []string

	// ExportsClusterRef is the logical cluster ID of the workspace the
	// APIExport landed in, which is what a consumer APIBinding must
	// reference. Reported because the workspace is configurable, so no
	// committed example can carry it.
	ExportsClusterRef string

	// KubeconfigSecret is the Secret, in the APIExport's workspace, holding
	// the kubeconfig the server should run with.
	KubeconfigSecret string
}

// Options configures Bootstrap.
type Options struct {
	// WorkspacePath is the absolute path the objects were installed
	// into, used only for reporting.
	WorkspacePath string

	// HostOverride replaces the scheme://host[:port] of the generated
	// kubeconfig, keeping the workspace path. Needed when init runs with
	// an externally reachable URL but the server will connect from inside
	// the cluster (or the reverse).
	HostOverride string

	// ServerUsers and ServerGroups are extra identities to grant the
	// controller role to, for deployments that run the server as something
	// other than the ServiceAccount minted here -- kcp-operator mounts a
	// client certificate, so its server arrives as a User.
	//
	// Empty means only the ServiceAccount is granted, which is correct when
	// the server uses the generated kubeconfig.
	ServerUsers  []string
	ServerGroups []string

	// Timeout bounds the apply-and-verify loop.
	Timeout time.Duration
}

// Bootstrap applies the embedded manifests to the workspace cfg points at,
// then waits for the APIExport and its endpoint slice to become usable.
func Bootstrap(ctx context.Context, cfg *rest.Config, opts Options) (*Result, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 3 * time.Minute
	}

	logger := klog.FromContext(ctx)
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build discovery client: %w", err)
	}
	cache := memory.NewMemCacheClient(discoveryClient)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cache)

	if err := preflight(discoveryClient, cfg.Host); err != nil {
		return nil, err
	}

	logger.Info("applying access virtual workspace assets", "workspace", opts.WorkspacePath)
	if err := applyFS(ctx, dynamicClient, mapper, cache, configapiexport.FS, configapiexport.Order); err != nil {
		return nil, fmt.Errorf("apply assets: %w", err)
	}

	logger.Info("applying controller identity", "serviceAccount", ControllerServiceAccount)
	if err := applyFS(ctx, dynamicClient, mapper, cache, configcontroller.FS, configcontroller.Order); err != nil {
		return nil, fmt.Errorf("apply controller identity: %w", err)
	}

	if len(opts.ServerUsers) > 0 || len(opts.ServerGroups) > 0 {
		logger.Info("granting the controller role to the configured server identity",
			"users", opts.ServerUsers, "groups", opts.ServerGroups)
		if err := applyServerIdentityBinding(ctx, dynamicClient, mapper, cache, opts.ServerUsers, opts.ServerGroups); err != nil {
			return nil, fmt.Errorf("grant server identity: %w", err)
		}
	}

	urls := waitForEndpointSlice(ctx, dynamicClient)

	exportsRef, err := exportsClusterRef(ctx, dynamicClient)
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes client: %w", err)
	}
	if err := createControllerKubeconfig(ctx, kubeClient, cfg, opts.HostOverride); err != nil {
		return nil, err
	}

	return &Result{
		WorkspacePath:          opts.WorkspacePath,
		APIExportEndpointSlice: APIExportName,
		VirtualWorkspaceURLs:   urls,
		ExportsClusterRef:      exportsRef,
		KubeconfigSecret:       ControllerKubeconfigSecret,
	}, nil
}

// createControllerKubeconfig waits for kcp to populate the ServiceAccount's
// token secret, then writes a kubeconfig for that identity into
// ControllerKubeconfigSecret in the same workspace. Idempotent: an existing
// secret is updated in place.
func createControllerKubeconfig(ctx context.Context, client kubernetes.Interface, cfg *rest.Config, hostOverride string) error {
	logger := klog.FromContext(ctx)

	var token []byte
	var caCert []byte
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		secret, err := client.CoreV1().Secrets(ControllerNamespace).Get(ctx, ControllerTokenSecret, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if len(secret.Data["token"]) == 0 {
			logger.V(2).Info("waiting for ServiceAccount token to be populated", "secret", ControllerTokenSecret)
			return false, nil
		}
		token = secret.Data["token"]
		caCert = secret.Data["ca.crt"]
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for ServiceAccount token %q: %w", ControllerTokenSecret, err)
	}

	server := cfg.Host
	if hostOverride != "" {
		if u, parseErr := url.Parse(cfg.Host); parseErr == nil && u.Path != "" {
			server = strings.TrimSuffix(hostOverride, "/") + u.Path
		} else {
			server = hostOverride
		}
		logger.Info("applying host override to generated kubeconfig", "server", server)
	}

	kubeconfig := clientcmdapi.Config{
		Kind:       "Config",
		APIVersion: "v1",
		Clusters: map[string]*clientcmdapi.Cluster{
			"workspace": {Server: server, CertificateAuthorityData: caCert},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"controller": {Token: string(token)},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"workspace": {Cluster: "workspace", AuthInfo: "controller"},
		},
		CurrentContext: "workspace",
	}

	raw, err := clientcmd.Write(kubeconfig)
	if err != nil {
		return fmt.Errorf("marshal kubeconfig: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ControllerKubeconfigSecret,
			Namespace: ControllerNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"kubeconfig": raw},
	}

	if _, err := client.CoreV1().Secrets(ControllerNamespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create kubeconfig secret: %w", err)
		}
		existing, getErr := client.CoreV1().Secrets(ControllerNamespace).Get(ctx, secret.Name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("get existing kubeconfig secret: %w", getErr)
		}
		secret.ResourceVersion = existing.ResourceVersion
		if _, err := client.CoreV1().Secrets(ControllerNamespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update kubeconfig secret: %w", err)
		}
		logger.Info("updated controller kubeconfig secret", "secret", secret.Name)
		return nil
	}

	logger.Info("created controller kubeconfig secret", "secret", secret.Name)
	return nil
}

func preflight(client discovery.DiscoveryInterface, host string) error {
	groups, err := client.ServerGroups()
	if err != nil {
		return fmt.Errorf("cannot reach kcp at %s: %T: %w", host, err, err)
	}
	for _, g := range groups.Groups {
		if g.Name == "apis.kcp.io" {
			return nil
		}
	}
	return fmt.Errorf("%s does not serve apis.kcp.io — is this a kcp workspace?", host)
}

func exportsClusterRef(ctx context.Context, client dynamic.Interface) (string, error) {
	lc, err := client.Resource(logicalClusterGVR).Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("reading LogicalCluster of the exports workspace: %w", err)
	}
	ref := lc.GetAnnotations()["kcp.io/cluster"]
	if ref == "" {
		return "", fmt.Errorf("LogicalCluster of the exports workspace carries no kcp.io/cluster annotation")
	}
	return ref, nil
}

func applyFS(ctx context.Context, client dynamic.Interface, mapper meta.RESTMapper, cache discovery.CachedDiscoveryInterface, fs embed.FS, order []string) error {
	logger := klog.FromContext(ctx)

	if err := verifyOrderCoversFS(fs, order); err != nil {
		return err
	}

	var lastErr error
	attempt := 0
	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		attempt++
		// Sequentially, stopping at the first failure: a later asset usually
		// depends on an earlier one, so continuing produces a pile of errors
		// whose first entry is the only real one.
		for _, name := range order {
			if err := applyFile(ctx, client, mapper, fs, name); err != nil {
				lastErr = err
				logger.V(2).Info("asset not applied yet, retrying", "file", name, "attempt", attempt, "err", err)
				cache.Invalidate()
				return false, nil
			}
		}
		logger.Info("assets applied", "files", len(order), "attempts", attempt)
		return true, nil
	})
	if err != nil && lastErr != nil {
		return fmt.Errorf("%w (last error: %v)", err, lastErr)
	}
	return err
}

// applyServerIdentityBinding binds ControllerClusterRole to identities the
// deployment names, alongside the committed ServiceAccount binding. The
// retry shape matches applyFS: the RBAC types are ordinary built-ins, but the
// workspace may still be settling when this runs.
func applyServerIdentityBinding(
	ctx context.Context,
	client dynamic.Interface,
	mapper meta.RESTMapper,
	cache discovery.CachedDiscoveryInterface,
	users, groups []string,
) error {
	binding := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRoleBinding",
		"metadata":   map[string]any{"name": ServerIdentityBinding},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "ClusterRole",
			"name":     ControllerClusterRole,
		},
		"subjects": serverSubjects(users, groups),
	}

	raw, err := yaml.Marshal(binding)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", ServerIdentityBinding, err)
	}

	var lastErr error
	err = wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := applyResource(ctx, client, mapper, raw); err != nil {
			lastErr = err
			cache.Invalidate()
			return false, nil //nolint:nilerr // retried until the context ends; lastErr is reported then
		}
		return true, nil
	})
	if err != nil && lastErr != nil {
		return fmt.Errorf("%w (last error: %v)", err, lastErr)
	}
	return err
}

// serverSubjects renders the subject list, deduplicated so that repeating a
// flag does not produce a binding the API server rejects.
func serverSubjects(users, groups []string) []any {
	var subjects []any
	seen := map[string]bool{}

	add := func(kind, name string) {
		if name == "" || seen[kind+"/"+name] {
			return
		}
		seen[kind+"/"+name] = true
		subjects = append(subjects, map[string]any{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     kind,
			"name":     name,
		})
	}

	for _, u := range users {
		add("User", u)
	}
	for _, g := range groups {
		add("Group", g)
	}
	return subjects
}

func verifyOrderCoversFS(fs embed.FS, order []string) error {
	listed := make(map[string]bool, len(order))
	for _, n := range order {
		listed[n] = true
	}

	entries, err := fs.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embedded assets: %w", err)
	}

	var missing []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if !listed[e.Name()] {
			missing = append(missing, e.Name())
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("embedded assets %v are not in the apply order; add them to Order", missing)
	}
	return nil
}

func applyFile(ctx context.Context, client dynamic.Interface, mapper meta.RESTMapper, fs embed.FS, filename string) error {
	raw, err := fs.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read %s: %w", filename, err)
	}

	reader := kubeyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(raw)))
	var errs []error
	for i := 1; ; i++ {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read document %d of %s: %w", i, filename, err)
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		if err := applyResource(ctx, client, mapper, doc); err != nil {
			errs = append(errs, fmt.Errorf("%s document %d: %w", filename, i, err))
		}
	}
	return kerrors.NewAggregate(errs)
}

func applyResource(ctx context.Context, client dynamic.Interface, mapper meta.RESTMapper, raw []byte) error {
	logger := klog.FromContext(ctx)

	u := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(raw, &u.Object); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	gvk := u.GroupVersionKind()
	if gvk.Kind == "" {
		return errors.New("manifest has no kind")
	}

	m, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("REST mapping for %s: %w", gvk, err)
	}

	resource := client.Resource(m.Resource).Namespace(u.GetNamespace())

	if createOnlyKinds[gvk.Kind] {
		return create(ctx, resource, u, gvk.Kind)
	}

	if _, err := resource.Get(ctx, u.GetName(), metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return create(ctx, resource, u, gvk.Kind)
		}
		return fmt.Errorf("get %s %s: %w", gvk.Kind, u.GetName(), err)
	}

	// Merged into the stored object rather than replacing it, because replacing it is not
	// idempotent: kcp defaults fields this manifest deliberately leaves out, and sending the
	// manifest back whole reads as clearing them.
	//
	// APIExportEndpointSlice is the case that bites. Its spec.export.path is omitted here so the
	// slice resolves to whichever workspace it was installed into; kcp fills the path in on
	// create, and then rejects an update that would empty it with "APIExport reference must not
	// be changed". Every re-run then fails — including the retry loop above and, because this runs
	// as an init container, every pod restart, which turns a restart into a permanent crash loop.
	patch, err := u.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal %s %s: %w", gvk.Kind, u.GetName(), err)
	}

	if _, err := resource.Patch(ctx, u.GetName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch %s %s: %w", gvk.Kind, u.GetName(), err)
	}
	logger.V(2).Info("updated", "kind", gvk.Kind, "name", u.GetName())
	return nil
}

func create(ctx context.Context, resource dynamic.ResourceInterface, u *unstructured.Unstructured, kind string) error {
	logger := klog.FromContext(ctx)

	_, err := resource.Create(ctx, u, metav1.CreateOptions{})
	switch {
	case err == nil:
		logger.Info("created", "kind", kind, "name", u.GetName())
		return nil
	case apierrors.IsAlreadyExists(err) && createOnlyKinds[kind]:
		logger.V(2).Info("exists", "kind", kind, "name", u.GetName())
		return nil
	default:
		return fmt.Errorf("create %s %s: %w", kind, u.GetName(), err)
	}
}

// waitForEndpointSlice returns the per-shard virtual workspace URLs the endpoint slice resolves to,
// waiting a while for them to appear but not insisting that they do.
//
// kcp only advertises endpoints on a slice once at least one workspace has an APIBinding to the
// export, so on a fresh installation the slice is legitimately empty: nothing has bound an export
// that has only just been created. Treating that as a failure deadlocks bootstrapping — the
// endpoints wait for a binding, the binding waits for a component that never finishes starting —
// and because this runs as an init container, the deadlock is a pod that never leaves Init.
//
// Nothing downstream needs the URLs. The server follows the slice with a watch and engages each
// cluster as it appears, so an empty slice at this point only means there is nothing to engage yet.
// They are still reported, because seeing them is the quickest confirmation that the export is
// being served.
func waitForEndpointSlice(ctx context.Context, client dynamic.Interface) []string {
	logger := klog.FromContext(ctx)

	// Short relative to the overall budget: this is worth pausing for, not worth spending the
	// whole timeout on.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var urls []string
	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		slice, err := client.Resource(apiExportEndpointSliceGVR).Get(ctx, APIExportName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		endpoints, found, err := unstructured.NestedSlice(slice.Object, "status", "endpoints")
		if err != nil || !found || len(endpoints) == 0 {
			logger.V(2).Info("waiting for APIExportEndpointSlice endpoints", "name", APIExportName)
			return false, nil //nolint:nilerr // a slice that cannot be read yet is waited for
		}

		urls = nil
		for _, e := range endpoints {
			entry, ok := e.(map[string]any)
			if !ok {
				continue
			}
			if u, ok := entry["url"].(string); ok && u != "" {
				urls = append(urls, u)
			}
		}
		return len(urls) > 0, nil
	})
	if err != nil {
		logger.Info("APIExportEndpointSlice has no virtual workspace endpoints yet; "+
			"this is expected until a workspace binds the export, and the server picks them up as they appear",
			"name", APIExportName)

		return nil
	}

	return urls
}

// VerifyAPIBinding reports whether the given workspace has an APIBinding to
// this APIExport with all of its permission claims accepted.
func VerifyAPIBinding(ctx context.Context, cfg *rest.Config, bindingName string) error {
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("build dynamic client: %w", err)
	}

	binding, err := client.Resource(schema.GroupVersionResource{
		Group: "apis.kcp.io", Version: "v1alpha2", Resource: "apibindings",
	}).Get(ctx, bindingName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get APIBinding %q: %w", bindingName, err)
	}

	declared, _, _ := unstructured.NestedSlice(binding.Object, "spec", "permissionClaims")
	applied, _, _ := unstructured.NestedSlice(binding.Object, "status", "appliedPermissionClaims")

	if len(applied) < len(declared) {
		return fmt.Errorf("APIBinding %q has %d permission claims declared but only %d applied; "+
			"the virtual workspace cannot see RBAC objects until they are accepted",
			bindingName, len(declared), len(applied))
	}
	return nil
}
