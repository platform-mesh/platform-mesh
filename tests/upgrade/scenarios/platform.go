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

package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The platform scenario: what the operator of the installation, not a
// tenant, has set up on the release, in the PlatformMesh object. An extra
// workspace for a provider, and a provider connection to it: a kubeconfig
// Platform Mesh writes into a Secret, scoped to the API the provider
// exports, for the controller of the provider to run with.
//
// After the upgrade the PlatformMesh object still says so and is Ready, the
// workspace and what is in it are there, and the kubeconfig works. Not just
// the one in the Secret now: also the copy taken on the release. The
// kubeconfig carries a token that is good for days, and whoever runs with a
// copy of it, a controller that read the Secret once or one outside of the
// cluster, must not be cut off by an upgrade that changes what signs or
// checks the token. The same goes for the admin kubeconfig of kcp and its
// client certificate.
//
// The objects are in manifests/platform-*.yaml.
const (
	platformWorkspace = "upgrade-platform"
	platformExport    = "platform.upgrade.platform-mesh.io"
	platformSecret    = "upgrade-platform-kubeconfig"

	// platformState is what the scenario remembers under (state.go): the
	// kubeconfigs as they were on the release, under these keys.
	platformState             = "platform"
	platformStateConnection   = "connection"
	platformStateAdmin        = "admin"
	platformMeshNamespace     = "platform-mesh-system"
	platformMeshName          = "platform-mesh"
	platformAdminSecret       = "kubeconfig-kcp-admin"
	platformKubeconfigKey     = "kubeconfig"
	platformAdminContext      = "base"
	platformGadgetAfterSuffix = "-after"
)

// Platform is the platform scenario.
func Platform() Scenario {
	path := providersPath + ":" + platformWorkspace
	return Scenario{
		Name: "platform",

		// ORDER MATTERS. Every step builds on the ones before it.
		Seed: []Step{
			// 1. The extra workspace: declared in the PlatformMesh
			//    object, created by the operator.
			{Name: "extra workspace", Run: seedExtraWorkspace(path)},
			// 2. After the workspace: the API the connection is scoped
			//    to has to be exported there before the operator can
			//    write a kubeconfig for it.
			{Name: "export in the extra workspace", Run: seedPlatformExport(path)},
			// 3. After the export: the connection, declared in the
			//    PlatformMesh object; the operator writes the Secret.
			{Name: "provider connection", Run: seedProviderConnection(path)},
			{Name: "platform mesh is ready", Run: validatePlatformMesh(path)},
			// 4. After the connection: its kubeconfig is used, and
			//    then remembered together with the one of the admin.
			{Name: "gadget with the kubeconfig of the connection", Run: seedGadget(path)},
			{Name: "remember the kubeconfigs", Run: seedKubeconfigs()},
		},

		Validate: []Step{
			{Name: "platform mesh is ready", Run: validatePlatformMesh(path)},
			{Name: "extra workspace", Run: validateWorkspace(providersPath, platformWorkspace)},
			{Name: "export in the extra workspace", Run: validateManifest(path, "platform-export.yaml")},
			{Name: "kubeconfig of the connection", Run: validateConnection(path, currentKubeconfig)},
			{Name: "kubeconfig of the connection, as of the release", Run: validateConnection(path, rememberedKubeconfig)},
			{Name: "kubeconfig of the admin, as of the release", Run: validateAdminKubeconfig()},
		},

		// A Gadget is created, read and deleted again with the
		// kubeconfig of the connection: Validate holds afterwards.
		After: []Step{
			{Name: "second gadget with the kubeconfig of the connection", Run: seedSecondGadget(path)},
			{Name: "delete the second gadget", Run: deleteSecondGadget(path)},
		},
	}
}

// kubeScheme has the types of the cluster Platform Mesh is installed into
// that the scenario works with.
var kubeScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(kubeScheme))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(kubeScheme))
}

// kubeClient is a client for the cluster Platform Mesh is installed into.
func kubeClient(ctx context.Context, env Environment) (ctrlruntimeclient.Client, error) {
	cfg, err := env.Kube(ctx)
	if err != nil {
		return nil, err
	}
	return ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: kubeScheme})
}

// platformMesh reads the PlatformMesh object.
func platformMesh(ctx context.Context, cl ctrlruntimeclient.Client) (*pmcorev1alpha1.PlatformMesh, error) {
	pm := &pmcorev1alpha1.PlatformMesh{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: platformMeshName}, pm); err != nil {
		return nil, err
	}
	return pm, nil
}

// declare appends an entry to the list at spec.kcp.<list> of the PlatformMesh
// object unless declared reports that it is there.
//
// The object is patched, and with the list alone. It is not updated through
// its Go type: Seed runs on the release with the types of this checkout,
// and an update would drop every field the release has and the checkout
// does not know, of an object that configures the whole installation.
func declare(ctx context.Context, env Environment, list string, entry any, declared func(*pmcorev1alpha1.PlatformMesh) bool) error {
	cl, err := kubeClient(ctx, env)
	if err != nil {
		return err
	}
	pm, err := platformMesh(ctx, cl)
	if err != nil {
		return err
	}
	if declared(pm) {
		return nil
	}
	// A merge patch replaces a list as a whole, so it carries the entries
	// that are there, read as they are and not through their Go type, and
	// the resourceVersion they were read at: a change in between fails
	// the patch, and the step is called again.
	raw := &unstructured.Unstructured{}
	raw.SetGroupVersionKind(pmcorev1alpha1.SchemeGroupVersion.WithKind("PlatformMesh"))
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(pm), raw); err != nil {
		return err
	}
	entries, _, err := unstructured.NestedSlice(raw.Object, "spec", "kcp", list)
	if err != nil {
		return fmt.Errorf("spec.kcp.%s of the PlatformMesh object: %w", list, err)
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"resourceVersion": raw.GetResourceVersion()},
		"spec":     map[string]any{"kcp": map[string]any{list: append(entries, entry)}},
	})
	if err != nil {
		return err
	}
	return cl.Patch(ctx, raw, ctrlruntimeclient.RawPatch(types.MergePatchType, patch))
}

func declaresWorkspace(pm *pmcorev1alpha1.PlatformMesh, path string) bool {
	return slices.ContainsFunc(pm.Spec.Kcp.ExtraWorkspaces, func(w pmcorev1alpha1.WorkspaceDeclaration) bool { return w.Path == path })
}

func declaresConnection(pm *pmcorev1alpha1.PlatformMesh) bool {
	return slices.ContainsFunc(pm.Spec.Kcp.ExtraProviderConnections, func(c pmcorev1alpha1.ProviderConnection) bool { return c.Secret == platformSecret })
}

// seedExtraWorkspace declares the workspace at path, of the type provider,
// in the PlatformMesh object, and waits for the operator to create it.
func seedExtraWorkspace(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		err := declare(ctx, env, "extraWorkspaces", pmcorev1alpha1.WorkspaceDeclaration{
			Path: path,
			Type: pmcorev1alpha1.WorkspaceTypeReference{Name: "provider", Path: "root"},
		}, func(pm *pmcorev1alpha1.PlatformMesh) bool { return declaresWorkspace(pm, path) })
		if err != nil {
			return fmt.Errorf("declaring the extra workspace %s: %w", path, err)
		}
		return workspaceReady(ctx, env, providersPath, platformWorkspace)
	}
}

// seedPlatformExport creates the export and the binding to it in the
// workspace at path, and waits for the binding to be bound.
func seedPlatformExport(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := adminClient(ctx, env, path)
		if err != nil {
			return err
		}
		if err := create(ctx, cl, "platform-export.yaml"); err != nil {
			return err
		}
		objects, err := existing(ctx, cl, "platform-export.yaml")
		if err != nil {
			return err
		}
		for _, obj := range objects {
			if obj.GetKind() != "APIBinding" {
				continue
			}
			if phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase"); phase != "Bound" {
				return fmt.Errorf("binding %s in %s is %q, not Bound", obj.GetName(), path, phase)
			}
		}
		return nil
	}
}

// seedProviderConnection declares the provider connection to the workspace
// at path in the PlatformMesh object, scoped to the export, and waits for
// the operator to write its Secret.
func seedProviderConnection(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		err := declare(ctx, env, "extraProviderConnections", pmcorev1alpha1.ProviderConnection{
			Path:           path,
			Secret:         platformSecret,
			APIExportNames: []string{platformExport},
			AdminAuth:      ptr.To(false),
		}, declaresConnection)
		if err != nil {
			return fmt.Errorf("declaring the provider connection to %s: %w", path, err)
		}
		_, err = currentKubeconfig(ctx, env)
		return err
	}
}

// validatePlatformMesh: the PlatformMesh object declares the extra
// workspace and the provider connection, and is Ready with them.
func validatePlatformMesh(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := kubeClient(ctx, env)
		if err != nil {
			return err
		}
		pm, err := platformMesh(ctx, cl)
		if err != nil {
			return err
		}
		if !declaresWorkspace(pm, path) || !declaresConnection(pm) {
			return steps.Terminal("the PlatformMesh object no longer declares the extra workspace %s (%t) and its provider connection (%t)",
				path, declaresWorkspace(pm, path), declaresConnection(pm))
		}
		// Ready for what the object says now, not for what it said
		// before the scenario declared its part.
		ready := meta.FindStatusCondition(pm.Status.Conditions, "Ready")
		if ready == nil || ready.Status != metav1.ConditionTrue || ready.ObservedGeneration != pm.Generation {
			return fmt.Errorf("the PlatformMesh object is not Ready at generation %d: %+v", pm.Generation, ready)
		}
		return nil
	}
}

// A kubeconfigSource returns a kubeconfig of the provider connection.
type kubeconfigSource func(ctx context.Context, env Environment) ([]byte, error)

// currentKubeconfig is the kubeconfig in the Secret of the provider
// connection, as the operator keeps it.
func currentKubeconfig(ctx context.Context, env Environment) ([]byte, error) {
	return secretKubeconfig(ctx, env, platformSecret)
}

// rememberedKubeconfig is the kubeconfig of the provider connection as
// seedKubeconfigs remembered it.
func rememberedKubeconfig(ctx context.Context, env Environment) ([]byte, error) {
	state, err := recall(ctx, env, platformState)
	if err != nil {
		return nil, err
	}
	return []byte(state[platformStateConnection]), nil
}

func secretKubeconfig(ctx context.Context, env Environment, name string) ([]byte, error) {
	cl, err := kubeClient(ctx, env)
	if err != nil {
		return nil, err
	}
	secret := &corev1.Secret{}
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: platformMeshNamespace, Name: name}, secret); err != nil {
		return nil, err
	}
	if len(secret.Data[platformKubeconfigKey]) == 0 {
		return nil, fmt.Errorf("secret %s has no %s", name, platformKubeconfigKey)
	}
	return secret.Data[platformKubeconfigKey], nil
}

// seedKubeconfigs remembers the kubeconfig of the provider connection and
// the one of the kcp admin, as they are on the release.
func seedKubeconfigs() stepFunc {
	return func(ctx context.Context, env Environment) error {
		connection, err := currentKubeconfig(ctx, env)
		if err != nil {
			return err
		}
		admin, err := secretKubeconfig(ctx, env, platformAdminSecret)
		if err != nil {
			return err
		}
		return remember(ctx, env, platformState, map[string]string{
			platformStateConnection: string(connection),
			platformStateAdmin:      string(admin),
		})
	}
}

// connectionClient is a client for the workspace at path with the
// credentials of a kubeconfig of the provider connection. The kubeconfig
// is written for use inside the cluster: its server is replaced by the
// front-proxy as the test reaches it, and the virtual workspace of the
// export by the workspace itself. The credentials are the ones of the
// kubeconfig.
func connectionClient(ctx context.Context, env Environment, kubeconfig []byte, path string) (ctrlruntimeclient.Client, error) {
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("the kubeconfig of the provider connection: %w", err)
	}
	return reachable(ctx, env, cfg, path)
}

// reachable returns a client with the credentials of cfg for the workspace
// at path, through the front-proxy as the test reaches it.
func reachable(ctx context.Context, env Environment, cfg *rest.Config, path string) (ctrlruntimeclient.Client, error) {
	kcp, err := env.KCP(ctx)
	if err != nil {
		return nil, err
	}
	front, err := url.Parse(kcp.Host)
	if err != nil {
		return nil, err
	}
	cfg = rest.CopyConfig(cfg)
	cfg.Host = strings.TrimSuffix(front.Scheme+"://"+front.Host, "/")
	// The CA of the kubeconfig is the one of the address inside the
	// cluster; the one of the front-proxy as reached from here is the
	// admin's.
	cfg.CAData, cfg.CAFile = kcp.CAData, kcp.CAFile
	return tenant.NewClient(cfg, path)
}

func gadget(suffix string) (*unstructured.Unstructured, error) {
	objects, err := manifest("platform-gadget.yaml")
	if err != nil {
		return nil, err
	}
	objects[0].SetName(objects[0].GetName() + suffix)
	return objects[0], nil
}

// seedGadget creates the Gadget of the manifest in the workspace at path,
// with the kubeconfig of the provider connection.
func seedGadget(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		return createGadget(ctx, env, path, "")
	}
}

func createGadget(ctx context.Context, env Environment, path, suffix string) error {
	kubeconfig, err := currentKubeconfig(ctx, env)
	if err != nil {
		return err
	}
	cl, err := connectionClient(ctx, env, kubeconfig, path)
	if err != nil {
		return err
	}
	obj, err := gadget(suffix)
	if err != nil {
		return err
	}
	if err := ctrlruntimeclient.IgnoreAlreadyExists(cl.Create(ctx, obj)); err != nil {
		return fmt.Errorf("creating gadget %s in %s with the kubeconfig of the provider connection: %w", obj.GetName(), path, err)
	}
	return cl.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(obj), obj)
}

// validateConnection: the kubeconfig of the provider connection, from the
// source, reads the Gadget of seedGadget in the workspace at path.
func validateConnection(path string, source kubeconfigSource) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kubeconfig, err := source(ctx, env)
		if err != nil {
			return err
		}
		cl, err := connectionClient(ctx, env, kubeconfig, path)
		if err != nil {
			return err
		}
		obj, err := gadget("")
		if err != nil {
			return err
		}
		if err := cl.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(obj), obj); err != nil {
			return fmt.Errorf("reading gadget %s in %s with the kubeconfig of the provider connection: %w", obj.GetName(), path, err)
		}
		return nil
	}
}

// validateAdminKubeconfig: the kubeconfig of the kcp admin, as
// seedKubeconfigs remembered it, still lists the organizations.
func validateAdminKubeconfig() stepFunc {
	return func(ctx context.Context, env Environment) error {
		state, err := recall(ctx, env, platformState)
		if err != nil {
			return err
		}
		raw, err := clientcmd.Load([]byte(state[platformStateAdmin]))
		if err != nil {
			return fmt.Errorf("the remembered kubeconfig of the kcp admin: %w", err)
		}
		cfg, err := clientcmd.NewNonInteractiveClientConfig(*raw, platformAdminContext, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
		if err != nil {
			return fmt.Errorf("the %s context of the remembered kubeconfig of the kcp admin: %w", platformAdminContext, err)
		}
		cfg.Host = strings.TrimSuffix(cfg.Host, "/")
		cl, err := tenant.NewClient(cfg, tenant.OrgsPath)
		if err != nil {
			return err
		}
		if err := cl.List(ctx, &pmcorev1alpha1.AccountList{}); err != nil {
			return fmt.Errorf("listing the organizations with the kubeconfig of the kcp admin as of the release: %w", err)
		}
		return nil
	}
}

// seedSecondGadget creates a second Gadget with the kubeconfig of the
// provider connection, and reads it back.
func seedSecondGadget(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		return createGadget(ctx, env, path, platformGadgetAfterSuffix)
	}
}

// deleteSecondGadget deletes the Gadget of seedSecondGadget with the
// kubeconfig of the provider connection, and waits until it is gone.
func deleteSecondGadget(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kubeconfig, err := currentKubeconfig(ctx, env)
		if err != nil {
			return err
		}
		cl, err := connectionClient(ctx, env, kubeconfig, path)
		if err != nil {
			return err
		}
		obj, err := gadget(platformGadgetAfterSuffix)
		if err != nil {
			return err
		}
		if err := ctrlruntimeclient.IgnoreNotFound(cl.Delete(ctx, obj)); err != nil {
			return fmt.Errorf("deleting gadget %s in %s: %w", obj.GetName(), path, err)
		}
		return gone(cl.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(obj), obj), fmt.Sprintf("gadget %s in %s", obj.GetName(), path))
	}
}
