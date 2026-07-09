// Package seed contains the idempotent kcp bootstrap logic used by the
// `provider-operator seed` subcommand. It is designed to run as a Kubernetes
// initContainer: it exits 0 once the kcp API surface is in place and non-zero
// otherwise, so the main container is blocked until kcp is seeded.
package seed

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/provider-operator/internal/config"
	"go.platform-mesh.io/provider-operator/manifests"
	pmsubs "go.platform-mesh.io/provider-operator/pkg/subroutines"
)

const (
	fieldOwner = "provider-operator-seed"

	// componentName is stamped onto every bootstrapped object via
	// corev1alpha1.OwnedByAnnotation to attribute ownership to this component.
	componentName = "provider-operator"

	workspaceReadyTimeout  = 3 * time.Minute
	workspaceReadyInterval = 2 * time.Second

	// rootWorkspacePath is the kcp root workspace, where the provider/providers
	// WorkspaceTypes live and under which the providers container is created.
	rootWorkspacePath = "root"

	// providersWorkspaceName / providersWorkspaceType are the parent container
	// workspace (root:providers) and its WorkspaceType. The Provider controller
	// creates per-provider tenant workspaces beneath it.
	providersWorkspaceName = "providers"
	providersWorkspaceType = "providers"

	// systemWorkspaceName is the default provider workspace (root:providers:system),
	// typed `provider`, where ManagedProviders register Provider resources when
	// their ProviderReference is unset. Being `provider`-typed, it auto-binds the
	// providers APIExport so the Provider kind is servable there.
	systemWorkspaceName       = "system"
	providerWorkspaceTypeName = "provider"
)

// Run idempotently seeds the kcp API surface and provider workspace structure,
// in dependency order:
//  1. ensures the API-surface workspace (root:platform-mesh-system) is Ready,
//  2. applies the APIResourceSchema, APIExport and the bind RBAC for the export,
//  3. ensures the APIExportEndpointSlice referencing the APIExport,
//  4. creates the provider/providers WorkspaceTypes (which bind the export, hence
//     must come after steps 2-3),
//  5. ensures the root:providers container workspace.
//
// kubeconfigPath, when non-empty, is the admin kubeconfig file to use (the
// initContainer case). When empty, it falls back to reading the cluster-admin
// secret via an in-cluster client, mirroring the operator's own mechanism.
func Run(ctx context.Context, log *logger.Logger, cfg *config.OperatorConfig, kubeconfigPath string) error {
	targetPath := cfg.Providers.ProvidersAPIExportEndpointSliceWorkspace
	parentPath, wsName, err := splitWorkspacePath(targetPath)
	if err != nil {
		return err
	}

	baseCfg, err := buildBaseConfig(cfg, kubeconfigPath)
	if err != nil {
		return fmt.Errorf("building kcp base config: %w", err)
	}

	helper := &pmsubs.Helper{}

	// Client scoped to the kcp root workspace: the platform-mesh-system and
	// providers workspaces are created here, and the provider WorkspaceTypes live
	// here (parentPath of the API-surface workspace is "root").
	rootClient, err := helper.NewKcpClient(rest.CopyConfig(baseCfg), rootWorkspacePath)
	if err != nil {
		return fmt.Errorf("creating kcp client for root workspace: %w", err)
	}

	// 1. Ensure the API-surface workspace (root:platform-mesh-system) exists and is Ready.
	if err := ensureWorkspaceReady(ctx, log, rootClient, parentPath, wsName, nil); err != nil {
		return err
	}

	// Client scoped to the API-surface workspace.
	targetClient, err := helper.NewKcpClient(rest.CopyConfig(baseCfg), targetPath)
	if err != nil {
		return fmt.Errorf("creating kcp client for workspace %q: %w", targetPath, err)
	}

	// 2. Apply the API surface, in dependency order: schema before the APIExport that
	//    references it, then the bind RBAC. All of this must exist BEFORE the
	//    WorkspaceTypes below — the `provider` type's defaultAPIBindings reference the
	//    providers APIExport, and kcp rejects a WorkspaceType that binds a missing
	//    export or one the caller lacks the `bind` permission on.
	for _, name := range []string{
		manifests.APIResourceSchemaFile,
		manifests.APIExportFile,
		manifests.ClusterRoleProvidersBindFile,
		manifests.ClusterRoleBindingProvidersBindFile,
	} {
		if err := applyManifest(ctx, log, targetClient, manifests.PlatformMeshSystemDir, name); err != nil {
			return err
		}
	}

	// 3. Ensure the APIExportEndpointSlice referencing the APIExport.
	if err := ensureEndpointSlice(ctx, log, targetClient, targetPath, cfg); err != nil {
		return err
	}

	// 4. WorkspaceTypes (create-if-not-exists) in root — safe now the export exists
	//    and bind is granted. The parent `providers` type must precede the
	//    root:providers workspace that references it. Shared structure: if something
	//    else already owns these (e.g. a full PM install), defer rather than overwrite.
	for _, name := range []string{manifests.WorkspaceTypeProvidersFile, manifests.WorkspaceTypeProviderFile} {
		if err := applyManifestIfAbsent(ctx, log, rootClient, manifests.RootDir, name); err != nil {
			return err
		}
	}

	// 5. The providers container workspace (root:providers), typed `providers`.
	providersType := &kcptenancyv1alpha1.WorkspaceTypeReference{
		Name: providersWorkspaceType,
		Path: rootWorkspacePath,
	}
	if err := ensureWorkspaceReady(ctx, log, rootClient, rootWorkspacePath, providersWorkspaceName, providersType); err != nil {
		return err
	}

	// 6. The default system provider workspace (root:providers:system), typed
	//    `provider` — the default target where ManagedProviders register Provider
	//    resources when ProviderReference is unset. Created under root:providers,
	//    so it needs a client scoped to that container.
	providersPath := rootWorkspacePath + ":" + providersWorkspaceName
	providersClient, err := helper.NewKcpClient(rest.CopyConfig(baseCfg), providersPath)
	if err != nil {
		return fmt.Errorf("creating kcp client for %q: %w", providersPath, err)
	}
	providerType := &kcptenancyv1alpha1.WorkspaceTypeReference{
		Name: providerWorkspaceTypeName,
		Path: rootWorkspacePath,
	}
	if err := ensureWorkspaceReady(ctx, log, providersClient, providersPath, systemWorkspaceName, providerType); err != nil {
		return err
	}

	log.Info().Str("workspace", targetPath).Msg("kcp seed completed")
	return nil
}

// buildBaseConfig returns a *rest.Config pointing at the kcp front-proxy base
// URL (without a /clusters/<path> suffix; NewKcpClient adds that). When
// kubeconfigPath is set it is loaded directly, otherwise the cluster-admin
// secret is read via an in-cluster client.
func buildBaseConfig(cfg *config.OperatorConfig, kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}

	inCluster, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("getting in-cluster config: %w", err)
	}
	runtimeClient, err := client.New(inCluster, client.Options{Scheme: pmsubs.GetClientScheme()})
	if err != nil {
		return nil, fmt.Errorf("creating in-cluster client: %w", err)
	}

	kcpURL := cfg.KCP.Url
	if kcpURL == "" {
		kcpURL = fmt.Sprintf("https://%s-front-proxy.%s:%s", cfg.KCP.FrontProxyName, cfg.KCP.Namespace, cfg.KCP.FrontProxyPort)
	}
	return pmsubs.BuildKubeconfigFromConfig(runtimeClient, &cfg.KCP, kcpURL)
}

// splitWorkspacePath splits a kcp workspace path like "root:platform-mesh-system"
// into its parent path ("root") and leaf name ("platform-mesh-system").
func splitWorkspacePath(wsPath string) (parent string, name string, err error) {
	idx := strings.LastIndex(wsPath, ":")
	if idx <= 0 || idx == len(wsPath)-1 {
		return "", "", fmt.Errorf("invalid workspace path %q: expected <parent>:<name>", wsPath)
	}
	return wsPath[:idx], wsPath[idx+1:], nil
}

// ensureWorkspaceReady creates the workspace if missing (in the parent-scoped
// client) and blocks until it reports phase Ready. When wsType is non-nil the
// workspace is created with that type; nil leaves Spec.Type empty so kcp assigns
// the parent's default type. wsType is only assigned on the mutate path when set,
// so an already-existing workspace (whose Spec.Type is immutable) is never
// spuriously updated — making this safe to run against a pre-seeded instance.
func ensureWorkspaceReady(ctx context.Context, log *logger.Logger, parentClient client.Client, parentPath, wsName string, wsType *kcptenancyv1alpha1.WorkspaceTypeReference) error {
	ws := &kcptenancyv1alpha1.Workspace{}
	ws.Name = wsName
	if _, err := controllerutil.CreateOrUpdate(ctx, parentClient, ws, func() error {
		// Stamp ownership only on creation; never re-attribute a workspace that
		// already exists (it may be owned by another component / a full PM install).
		if ws.GetResourceVersion() == "" {
			corev1alpha1.SetOwnedBy(ws, componentName)
		}
		if wsType != nil {
			ws.Spec.Type = wsType
		}
		return nil
	}); err != nil {
		return fmt.Errorf("ensuring workspace %s:%s: %w", parentPath, wsName, err)
	}

	log.Info().Str("parent", parentPath).Str("workspace", wsName).Msg("waiting for workspace to become Ready")
	err := wait.PollUntilContextTimeout(ctx, workspaceReadyInterval, workspaceReadyTimeout, true, func(ctx context.Context) (bool, error) {
		cur := &kcptenancyv1alpha1.Workspace{}
		if err := parentClient.Get(ctx, types.NamespacedName{Name: wsName}, cur); err != nil {
			return false, nil
		}
		return string(cur.Status.Phase) == "Ready", nil
	})
	if err != nil {
		return fmt.Errorf("waiting for workspace %s:%s to become Ready: %w", parentPath, wsName, err)
	}
	return nil
}

// readManifest reads and decodes one embedded YAML manifest into an unstructured.
func readManifest(dir, fileName string) (*unstructured.Unstructured, error) {
	data, err := manifests.KCPFS.ReadFile(path.Join(dir, fileName))
	if err != nil {
		return nil, fmt.Errorf("reading embedded manifest %q: %w", fileName, err)
	}
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(data, &obj.Object); err != nil {
		return nil, fmt.Errorf("decoding manifest %q: %w", fileName, err)
	}
	// Managed fields are server-owned; must not be sent on apply/create.
	obj.SetManagedFields(nil)
	return obj, nil
}

// applyManifest server-side-applies one embedded YAML manifest into the given
// (workspace-scoped) client, making the operation idempotent. Use for resources
// this operator owns (the APIExport/schema).
func applyManifest(ctx context.Context, log *logger.Logger, cl client.Client, dir, fileName string) error {
	obj, err := readManifest(dir, fileName)
	if err != nil {
		return err
	}
	corev1alpha1.SetOwnedBy(obj, componentName)
	if err := cl.Patch(ctx, obj, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("applying manifest %q (%s/%s): %w", fileName, obj.GetKind(), obj.GetName(), err)
	}
	log.Info().Str("kind", obj.GetKind()).Str("name", obj.GetName()).Msg("applied manifest")
	return nil
}

// applyManifestIfAbsent creates the manifest only when it does not already exist,
// deferring to whatever owns it otherwise. Use for shared structure (the provider
// WorkspaceTypes) that a full PM install may already manage — a server-side apply
// there would fight for ownership; create-if-not-exists coexists cleanly.
func applyManifestIfAbsent(ctx context.Context, log *logger.Logger, cl client.Client, dir, fileName string) error {
	obj, err := readManifest(dir, fileName)
	if err != nil {
		return err
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	getErr := cl.Get(ctx, types.NamespacedName{Name: obj.GetName()}, existing)
	if getErr == nil {
		log.Info().Str("kind", obj.GetKind()).Str("name", obj.GetName()).Msg("manifest already exists, skipping")
		return nil
	}
	if !apierrors.IsNotFound(getErr) {
		return fmt.Errorf("checking manifest %q (%s/%s): %w", fileName, obj.GetKind(), obj.GetName(), getErr)
	}

	corev1alpha1.SetOwnedBy(obj, componentName)
	if err := cl.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating manifest %q (%s/%s): %w", fileName, obj.GetKind(), obj.GetName(), err)
	}
	log.Info().Str("kind", obj.GetKind()).Str("name", obj.GetName()).Msg("created manifest")
	return nil
}

// ensureEndpointSlice creates the APIExportEndpointSlice referencing the providers
// APIExport in the same workspace, only when it does not already exist.
// spec.export is immutable in kcp (kcp defaults spec.export.path on create), so a
// CreateOrUpdate that re-sets it fails with "APIExport reference must not be
// changed" on re-runs — hence create-if-not-exists.
func ensureEndpointSlice(ctx context.Context, log *logger.Logger, cl client.Client, targetPath string, cfg *config.OperatorConfig) error {
	name := cfg.Providers.ProvidersAPIExportEndpointSliceName

	existing := &kcpapisv1alpha1.APIExportEndpointSlice{}
	getErr := cl.Get(ctx, types.NamespacedName{Name: name}, existing)
	if getErr == nil {
		log.Info().Str("name", name).Str("workspace", targetPath).Msg("APIExportEndpointSlice already exists, skipping")
		return nil
	}
	if !apierrors.IsNotFound(getErr) {
		return fmt.Errorf("checking APIExportEndpointSlice %q in %s: %w", name, targetPath, getErr)
	}

	slice := &kcpapisv1alpha1.APIExportEndpointSlice{}
	slice.Name = name
	// APIExport lives in the same workspace, so Path may be left empty.
	slice.Spec.APIExport = kcpapisv1alpha1.ExportBindingReference{Name: name}
	corev1alpha1.SetOwnedBy(slice, componentName)
	if err := cl.Create(ctx, slice); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating APIExportEndpointSlice %q in %s: %w", name, targetPath, err)
	}
	log.Info().Str("name", name).Str("workspace", targetPath).Msg("created APIExportEndpointSlice")
	return nil
}
