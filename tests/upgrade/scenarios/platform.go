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
	"fmt"
	"slices"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
	"go.platform-mesh.io/platform-mesh/tests/framework/steps"
	"go.platform-mesh.io/platform-mesh/tests/framework/tenant"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

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

	platformExportManifest = "platform-export.yaml"
	platformGadgetManifest = "platform-gadget.yaml"

	// platformState is what the scenario remembers under (state.go): the
	// kubeconfigs as they were on the release, under these keys.
	platformState           = "platform"
	platformStateConnection = "connection"
	platformStateAdmin      = "admin"

	// gadgetAfter names the copy of the Gadget that After creates and
	// deletes.
	gadgetAfter = "-after"
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
			{Name: "gadget with the kubeconfig of the connection", Run: withConnection(path, currentKubeconfig, createGadget(""))},
			{Name: "remember the kubeconfigs", Run: seedKubeconfigs()},
		},

		Validate: []Step{
			{Name: "platform mesh is ready", Run: validatePlatformMesh(path)},
			{Name: "extra workspace", Run: validateWorkspace(providersPath, platformWorkspace)},
			{Name: "export in the extra workspace", Run: validateManifest(path, platformExportManifest)},
			{Name: "kubeconfig of the connection", Run: withConnection(path, currentKubeconfig, readGadget)},
			{Name: "kubeconfig of the connection, as of the release", Run: withConnection(path, rememberedKubeconfig, readGadget)},
			{Name: "kubeconfig of the admin, as of the release", Run: validateAdminKubeconfig()},
		},

		// A Gadget is created, read and deleted again with the
		// kubeconfig of the connection: Validate holds afterwards.
		After: []Step{
			{Name: "second gadget with the kubeconfig of the connection", Run: withConnection(path, currentKubeconfig, createGadget(gadgetAfter))},
			{Name: "delete the second gadget", Run: withConnection(path, currentKubeconfig, deleteGadget(gadgetAfter))},
		},
	}
}

func declaresWorkspace(mesh *pmcorev1alpha1.PlatformMesh, path string) bool {
	return slices.ContainsFunc(mesh.Spec.Kcp.ExtraWorkspaces, func(w pmcorev1alpha1.WorkspaceDeclaration) bool { return w.Path == path })
}

func declaresConnection(mesh *pmcorev1alpha1.PlatformMesh) bool {
	return slices.ContainsFunc(mesh.Spec.Kcp.ExtraProviderConnections, func(c pmcorev1alpha1.ProviderConnection) bool { return c.Secret == platformSecret })
}

// declare appends the entry to spec.kcp.<list> of the PlatformMesh object
// unless declared reports that it is there. It patches the list alone
// (pm.AppendToPlatformMesh): Seed runs on the release with the types of
// this checkout, and must not write the object back through them.
func declare(ctx context.Context, env Environment, list string, entry any, declared func(*pmcorev1alpha1.PlatformMesh) bool) error {
	cl, err := kubeClient(ctx, env)
	if err != nil {
		return err
	}
	mesh, err := pm.GetPlatformMesh(ctx, cl)
	if err != nil {
		return err
	}
	if declared(mesh) {
		return nil
	}
	return pm.AppendToPlatformMesh(ctx, cl, entry, "spec", "kcp", list)
}

// seedExtraWorkspace declares the workspace at path, of the type provider,
// in the PlatformMesh object, and waits for the operator to create it.
func seedExtraWorkspace(path string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		err := declare(ctx, env, "extraWorkspaces", pmcorev1alpha1.WorkspaceDeclaration{
			Path: path,
			Type: pmcorev1alpha1.WorkspaceTypeReference{Name: "provider", Path: "root"},
		}, func(mesh *pmcorev1alpha1.PlatformMesh) bool { return declaresWorkspace(mesh, path) })
		if err != nil {
			return err
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
		if err := create(ctx, cl, platformExportManifest); err != nil {
			return err
		}
		// The binding is named like the export it binds.
		return wait.CheckAPIBindingBound(ctx, cl, platformExport)
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
			return err
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
		mesh, err := pm.CheckPlatformMeshReady(ctx, cl)
		if mesh != nil && (!declaresWorkspace(mesh, path) || !declaresConnection(mesh)) {
			return steps.Terminal("the PlatformMesh object no longer declares the extra workspace %s (%t) and its provider connection (%t)",
				path, declaresWorkspace(mesh, path), declaresConnection(mesh))
		}
		return err
	}
}

// A kubeconfigSource returns a kubeconfig of the provider connection.
type kubeconfigSource func(ctx context.Context, env Environment) ([]byte, error)

// currentKubeconfig is the kubeconfig in the Secret of the provider
// connection, as the operator keeps it.
func currentKubeconfig(ctx context.Context, env Environment) ([]byte, error) {
	cl, err := kubeClient(ctx, env)
	if err != nil {
		return nil, err
	}
	return pm.SecretKubeconfig(ctx, cl, platformSecret)
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

// seedKubeconfigs remembers the kubeconfig of the provider connection and
// the one of the kcp admin, as they are on the release.
func seedKubeconfigs() stepFunc {
	return func(ctx context.Context, env Environment) error {
		connection, err := currentKubeconfig(ctx, env)
		if err != nil {
			return err
		}
		cl, err := kubeClient(ctx, env)
		if err != nil {
			return err
		}
		admin, err := pm.SecretKubeconfig(ctx, cl, pm.AdminKubeconfigSecret)
		if err != nil {
			return err
		}
		return remember(ctx, env, platformState, map[string]string{
			platformStateConnection: string(connection),
			platformStateAdmin:      string(admin),
		})
	}
}

// withConnection is a step that runs do with a client for the workspace at
// path that has the credentials of a kubeconfig of the provider connection,
// from the source. The kubeconfig is written for use inside the cluster;
// pm.Reachable makes it work from here.
func withConnection(path string, source kubeconfigSource, do func(ctx context.Context, cl ctrlruntimeclient.Client) error) stepFunc {
	return func(ctx context.Context, env Environment) error {
		kubeconfig, err := source(ctx, env)
		if err != nil {
			return err
		}
		kcp, err := env.KCP(ctx)
		if err != nil {
			return err
		}
		cfg, err := pm.Reachable(kcp, kubeconfig, path)
		if err != nil {
			return err
		}
		cl, err := pm.NewClient(cfg)
		if err != nil {
			return err
		}
		if err := do(ctx, cl); err != nil {
			return fmt.Errorf("with the kubeconfig of the provider connection: %w", err)
		}
		return nil
	}
}

// createGadget creates the Gadget of the manifest, under its name with the
// suffix, and reads it back.
func createGadget(suffix string) func(ctx context.Context, cl ctrlruntimeclient.Client) error {
	return func(ctx context.Context, cl ctrlruntimeclient.Client) error {
		return createCopy(ctx, cl, platformGadgetManifest, suffix)
	}
}

// deleteGadget deletes the Gadget createGadget made with the suffix, and
// waits until it is gone.
func deleteGadget(suffix string) func(ctx context.Context, cl ctrlruntimeclient.Client) error {
	return func(ctx context.Context, cl ctrlruntimeclient.Client) error {
		return deleteCopy(ctx, cl, platformGadgetManifest, suffix)
	}
}

// readGadget reads the Gadget of the manifest.
func readGadget(ctx context.Context, cl ctrlruntimeclient.Client) error {
	_, err := existing(ctx, cl, platformGadgetManifest)
	return err
}

// validateAdminKubeconfig: the kubeconfig of the kcp admin, as
// seedKubeconfigs remembered it, still lists the organizations.
func validateAdminKubeconfig() stepFunc {
	return func(ctx context.Context, env Environment) error {
		state, err := recall(ctx, env, platformState)
		if err != nil {
			return err
		}
		cfg, err := pm.AdminConfig([]byte(state[platformStateAdmin]))
		if err != nil {
			return err
		}
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
