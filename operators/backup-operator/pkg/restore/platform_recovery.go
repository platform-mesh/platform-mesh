package restore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	platformRecoverySubroutineName = "platform-recovery"

	kcpAdminSecretName       = "kubeconfig-kcp-admin"
	kcpWebhookSecretName     = "kcp-webhook-secret"
	rebacServingSecretName   = "rebac-authz-webhook-cert"
	rebacWebhookHost         = "rebac-authz-webhook.platform-mesh-system.svc.cluster.local"
	openFGAServiceURLDefault = "http://openfga.platform-mesh-system.svc.cluster.local:8080"
	kcpBaseURLDefault        = "https://frontproxy-front-proxy.platform-mesh-system:8443"
	// APIExportEndpointSlices are published by the APIExport provider, not by
	// each consumer workspace. The core.platform-mesh.io export is provided by
	// root:platform-mesh-system. Starting ReBAC in root:orgs is a dead end: the
	// physical orgs shard has no endpoint slice, while the front-proxy route is
	// itself protected by the ReBAC webhook that ReBAC is trying to start.
	kcpReBACBootstrapURLDefault = "https://root-proxy.platform-mesh-system.svc.cluster.local:6443/clusters/root:platform-mesh-system"
	orgsLogicalClusterPath      = "root:orgs"
	platformSystemPath          = "root:platform-mesh-system"
	virtualWorkspaceSchemaPath  = platformSystemPath

	webhookRestartAnnotation        = "restore.platform-mesh.io/webhook-restarted-for"
	tokenRestartAnnotation          = "restore.platform-mesh.io/token-restarted-for"
	tokenRefreshAnnotation          = "restore.platform-mesh.io/token-refreshed-for"
	rebacBootstrapRestartAnnotation = "restore.platform-mesh.io/rebac-bootstrap-restarted-for"
	// v7 moves ReBAC endpoint discovery from the consumer organizations
	// workspace to the core APIExport provider workspace.
	rebacBootstrapCredentialAnnotation    = "restore.platform-mesh.io/rebac-bootstrap-credential-v7-for"
	rebacCredentialRestartAnnotation      = "restore.platform-mesh.io/rebac-credential-restarted-v7-for"
	virtualWorkspacesCredentialAnnotation = "restore.platform-mesh.io/virtual-workspaces-bootstrap-credential-v1-for"
	virtualWorkspacesRestartAnnotation    = "restore.platform-mesh.io/virtual-workspaces-bootstrap-restarted-v1-for"
	extensionManagerCredentialAnnotation  = "restore.platform-mesh.io/extension-manager-bootstrap-credential-v1-for"
	extensionManagerRestartAnnotation     = "restore.platform-mesh.io/extension-manager-bootstrap-restarted-v1-for"
	// v2 restores the provider-kubeconfig virtual-workspace path in addition
	// to replacing stale source credentials and CA data.
	portalCredentialAnnotation          = "restore.platform-mesh.io/portal-bootstrap-credential-v2-for"
	portalRestartAnnotation             = "restore.platform-mesh.io/portal-bootstrap-restarted-v2-for"
	frontProxyRestartAnnotation         = "restore.platform-mesh.io/front-proxy-restarted-v1-for"
	kcpWebhookBootstrapDisabledKey      = "kcpWebhookBootstrapDisabledFor"
	kcpWebhookBootstrapRestoredKey      = "kcpWebhookBootstrapRestoredFor"
	kcpIdentityRepairWebhookDisabledKey = "kcpIdentityRepairWebhookDisabledV2For"
	kcpIdentityRepairWebhookRestoredKey = "kcpIdentityRepairWebhookRestoredV2For"
	kcpWebhookArgumentPrefix            = "kcpWebhookArgument."
)

type PlatformRecoverySubroutine struct {
	name       string
	client     ctrlruntimeclient.Client
	httpClient *http.Client
}

// recoveryWait is the common outcome for an eventually-consistent recovery
// action. Individual recovery domains add only the state they need.
type recoveryWait struct {
	requeueAfter  time.Duration
	requeueReason string
}

const (
	kcpRBACCreateOnly = false
	kcpRBACReconcile  = true
)

var storeGVR = schema.GroupVersionResource{
	Group:    "core.platform-mesh.io",
	Version:  "v1alpha1",
	Resource: "stores",
}

// recoverOpenFGAStores clears stale restored Store state and waits until the
// security operator has recreated usable OpenFGA stores and models.
func (p *PlatformRecoverySubroutine) recoverOpenFGAStores(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (recoveryWait, error) {
	done, err := p.ensureOpenFGAStores(ctx, pr)
	if err != nil {
		return recoveryWait{}, err
	}
	if !done {
		return recoveryWait{10 * time.Second, "waiting for OpenFGA store recovery"}, nil
	}
	return recoveryWait{}, nil
}

func NewPlatformRecoverySubroutine(cli ctrlruntimeclient.Client) *PlatformRecoverySubroutine {
	return &PlatformRecoverySubroutine{
		name:   platformRecoverySubroutineName,
		client: cli,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *PlatformRecoverySubroutine) GetName() string {
	return p.name
}

func (p *PlatformRecoverySubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	pr, ok := obj.(*v1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected v1alpha1.PlatformRestore, got %T", obj)
	}

	if restoreTerminal(pr) ||
		(conditionIsTrue(pr, conditionPlatformRecovered) &&
			conditionIsTrue(pr, conditionKCPVirtualWorkspaceClaimsRecovered)) {
		return subroutines.OK(), false, nil
	}

	log := logger.LoadLoggerFromContext(ctx)

	if !conditionIsTrue(pr, conditionControlPlaneRestarted) {
		log.Info().
			Str("step", "control-plane-restart").
			Msg("platform recovery is waiting")

		return subroutines.StopWithRequeue(
			5*time.Second,
			"waiting for control-plane restart",
		), false, nil
	}

	log.Info().
		Str("step", "rebac-webhook-trust").
		Msg("starting platform recovery step")

	changed, err := p.ensureReBACWebhookTrust(ctx, pr)
	if err != nil {
		log.Error().
			Err(err).
			Str("step", "rebac-webhook-trust").
			Msg("platform recovery step failed")

		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false, nil
	}
	if changed {
		log.Info().
			Str("step", "rebac-webhook-trust").
			Msg("webhook trust was repaired; waiting for KCP workloads")

		return subroutines.StopWithRequeue(
			10*time.Second,
			"waiting for kcp workloads after webhook CA repair",
		), false, nil
	}

	log.Info().
		Str("step", "kcp-webhook-consumers").
		Msg("checking platform recovery prerequisites")

	ready, err := workloadsReady(ctx, p.client, kcpWebhookConsumers)
	if err != nil {
		log.Error().
			Err(err).
			Str("step", "kcp-webhook-consumers").
			Msg("platform recovery step failed")

		return subroutines.OK(), false, err
	}
	if !ready {
		log.Info().
			Str("step", "kcp-webhook-consumers").
			Msg("platform recovery is waiting")

		return subroutines.StopWithRequeue(
			10*time.Second,
			"waiting for kcp webhook consumers",
		), false, nil
	}

	bootstrapReady, err := p.ensureKCPWebhookBootstrapDisabled(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false, nil
	}
	if !bootstrapReady {
		return subroutines.StopWithRequeue(5*time.Second, "waiting for KCP webhook bootstrap disable"), false, nil
	}

	claimsProgress, err := p.recoverKCPClaims(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false, nil
	}
	if claimsProgress.requeueReason != "" {
		return subroutines.StopWithRequeue(claimsProgress.requeueAfter, claimsProgress.requeueReason), false, nil
	}
	claimsChanged := claimsProgress.changed

	consumerProgress, err := p.repairBootstrapConsumers(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false, nil
	}
	if consumerProgress.requeueReason != "" {
		return subroutines.StopWithRequeue(consumerProgress.requeueAfter, consumerProgress.requeueReason), false, nil
	}

	rebacProgress, err := p.ensureReBACAvailability(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false, nil
	}
	if rebacProgress.requeueReason != "" {
		return subroutines.StopWithRequeue(rebacProgress.requeueAfter, rebacProgress.requeueReason), false, nil
	}

	changed, err = p.ensureVirtualWorkspaceSchemaWorkspace(ctx)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false, nil
	}
	if changed {
		return subroutines.StopWithRequeue(10*time.Second, "waiting for virtual-workspaces schema workspace update"), false, nil
	}

	log.Info().
		Str("step", "application-tokens").
		Msg("starting platform recovery step")

	tokenProgress, err := p.recoverApplicationTokens(ctx, pr)
	if err != nil {
		var workloadErr applicationWorkloadCheckError
		if errors.As(err, &workloadErr) {
			return subroutines.OK(), false, workloadErr.Unwrap()
		}
		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false, nil
	}
	if tokenProgress.requeueReason != "" {
		return subroutines.StopWithRequeue(tokenProgress.requeueAfter, tokenProgress.requeueReason), false, nil
	}

	bootstrapProgress, err := p.closeKCPBootstrapWindow(ctx, pr)
	if err != nil {
		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false, nil
	}
	if bootstrapProgress.requeueReason != "" {
		return subroutines.StopWithRequeue(bootstrapProgress.requeueAfter, bootstrapProgress.requeueReason), false, nil
	}

	log.Info().
		Str("step", "openfga-stores").
		Msg("starting platform recovery step")

	openFGAProgress, err := p.recoverOpenFGAStores(ctx, pr)
	if err != nil {
		log.Error().
			Err(err).
			Str("step", "openfga-stores").
			Msg("platform recovery step failed")

		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false, nil
	}
	if openFGAProgress.requeueReason != "" {
		log.Info().
			Str("step", "openfga-stores").
			Msg("platform recovery is waiting")

		return subroutines.StopWithRequeue(
			openFGAProgress.requeueAfter,
			openFGAProgress.requeueReason,
		), false, nil
	}

	organizationProgress, err := p.recoverOrganizations(ctx, pr)
	if err != nil {
		log.Error().
			Err(err).
			Str("step", "organizations-recovery").
			Msg("platform recovery step failed")
		return subroutines.StopWithRequeue(10*time.Second, err.Error()), false, nil
	}
	if organizationProgress.requeueReason != "" {
		return subroutines.StopWithRequeue(organizationProgress.requeueAfter, organizationProgress.requeueReason), false, nil
	}

	statusChanged := markPhaseReady(
		pr,
		conditionPlatformRecovered,
		"PlatformRecovered",
		"KCP webhook trust, application tokens, OpenFGA stores, accounts, and organization identity providers were recovered",
	)
	statusChanged = statusChanged || claimsChanged

	log.Info().
		Str("platformRestore", pr.Name).
		Msg("platform recovery completed")

	return subroutines.OK(), statusChanged, nil
}

// ensureKCPAPIClaims verifies the exact KCP permission used by
// virtual-workspaces. A restored APIBinding can look Bound while carrying
// permission-claim status from the source platform. A missing claim is exposed
// by KCP as either Forbidden or NotFound. Recreating only the platform's two
// internal bindings lets KCP derive current claims from the restored exports.
func (p *PlatformRecoverySubroutine) ensureKCPAPIClaims(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	log := logger.LoadLoggerFromContext(ctx)
	// Requests made through the front-proxy are first authorized in the root
	// workspace before KCP routes them to the physical workspace.  Installing
	// the reader only in the latter lets the recovery credential access a shard
	// directly, but still leaves virtual-workspaces unable to read its schema
	// through the front-proxy.
	rootRecoveryClient, err := p.kcpRecoveryDynamicClient(ctx, "root")
	if err != nil {
		return false, err
	}
	changed, err := ensureKCPVirtualWorkspaceSchemaReader(ctx, rootRecoveryClient)
	if err != nil {
		return false, fmt.Errorf("ensure root virtual-workspace schema reader: %w", err)
	}
	if changed {
		return false, nil
	}
	orgsRecoveryClient, err := p.kcpRecoveryDynamicClient(ctx, orgsLogicalClusterPath)
	if err != nil {
		return false, err
	}
	changed, err = ensureKCPVirtualWorkspaceSchemaReader(ctx, orgsRecoveryClient)
	if err != nil {
		return false, fmt.Errorf("ensure orgs virtual-workspace schema reader: %w", err)
	}
	if changed {
		return false, nil
	}

	recoveryClient, err := p.kcpRecoveryDynamicClient(ctx, virtualWorkspaceSchemaPath)
	if err != nil {
		return false, err
	}
	changed, err = ensureKCPVirtualWorkspaceSchemaReader(ctx, recoveryClient)
	if err != nil {
		return false, err
	}
	if changed {
		return false, nil
	}
	// A bound API is authorized in the provider APIExport workspace with the
	// caller rewritten as apis.kcp.io:binding:<user>. Consumer-workspace RBAC
	// alone therefore cannot authorize Portal's read of a bound core API.
	// Grant the narrowly-scoped reader in each provider workspace as well.
	providerReadersReady, err := p.ensureKCPAPIExportProviderReaders(ctx, recoveryClient)
	if err != nil {
		return false, err
	}
	if !providerReadersReady {
		return false, nil
	}
	// Velero restores APIBinding status as data. A binding can consequently
	// report Bound while its boundResources are from an older APIExport revision.
	// The APIExport virtual workspace denies resources that are absent from this
	// list before normal RBAC is evaluated. In particular, Portal cannot read the
	// identity provider configuration until this current export is reprocessed.
	identityBindingReady, err := p.ensureIdentityProviderAPIBindingReady(ctx, pr)
	if err != nil {
		return false, err
	}
	if !identityBindingReady {
		return false, nil
	}
	// The recovery client targets the destination shard and physical logical
	// cluster ID. Do not rebuild the restored kubeconfig's front-proxy URL here:
	// it may still point at an endpoint that cannot authenticate root-client.
	if _, err := recoveryClient.Resource(apiResourceSchemaGVR).List(ctx, metav1.ListOptions{Limit: 1}); err == nil {
		return true, nil
	} else if !apierrors.IsForbidden(err) && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("list KCP APIResourceSchemas: %w", err)
	}

	// Do not delete a restored APIBinding: its finalizer deletes the bound
	// resources, including identity configuration, and can deadlock recovery.
	// A later recovery step must repair permission claims in place.
	bindings := recoveryClient.Resource(apiBindingGVR)
	for _, name := range []string{"core.platform-mesh.io", "system.platform-mesh.io"} {
		binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("get APIBinding %s while checking KCP claims: %w", name, err)
		}
		annotations := binding.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		if annotations["restore.platform-mesh.io/apibinding-claims-checked-for"] == string(pr.UID) {
			continue
		}
		annotations["restore.platform-mesh.io/apibinding-claims-checked-for"] = string(pr.UID)
		binding.SetAnnotations(annotations)
		if _, err := bindings.Update(ctx, binding, metav1.UpdateOptions{}); err != nil {
			return false, fmt.Errorf("update APIBinding %s while checking KCP claims: %w", name, err)
		}
		log.Warn().
			Str("apiBinding", name).
			Msg("checked restored APIBinding without deleting bound identity resources")
	}
	// The restore-owned reader RBAC is now installed in every consumer and
	// provider workspace. Some restored APIBindings keep source status while
	// their virtual workspace is re-established, so requiring this direct list
	// to succeed here can deadlock before ReBAC is able to start. Continue with
	// the bootstrap; validateKCPDiscovery later verifies the exact Portal reads
	// after the webhook and its path-aware provider are running.
	return true, nil
}

// ensureIdentityProviderAPIBindingReady is the narrow recovery gate for the
// API used by Portal to load its identity-provider configuration. The resource
// belongs to the system.platform-mesh.io export even though its API group is
// core.platform-mesh.io. Adding it to the similarly named core export creates
// a permanent APIBinding NamingConflicts condition.
func (p *PlatformRecoverySubroutine) ensureIdentityProviderAPIBindingReady(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	consumer, err := p.kcpRecoveryDynamicClient(ctx, platformSystemPath)
	if err != nil {
		return false, fmt.Errorf("create identity-provider APIBinding recovery client: %w", err)
	}

	identityExportPath, err := apiBindingExportPath(ctx, consumer, systemAPIBindingName)
	if err != nil {
		return false, err
	}
	identityProvider, err := p.kcpRecoveryDynamicClient(ctx, identityExportPath)
	if err != nil {
		return false, fmt.Errorf("create APIExport provider client for %s: %w", identityExportPath, err)
	}

	coreExportPath, err := apiBindingExportPath(ctx, consumer, coreAPIBindingName)
	if err != nil {
		return false, err
	}
	coreProvider, err := p.kcpRecoveryDynamicClient(ctx, coreExportPath)
	if err != nil {
		return false, fmt.Errorf("create APIExport provider client for %s: %w", coreExportPath, err)
	}

	return p.ensureIdentityProviderAPIBindingResources(ctx, pr, consumer, identityProvider, coreProvider)
}

func (p *PlatformRecoverySubroutine) ensureIdentityProviderAPIBindingResources(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
	consumer dynamic.Interface,
	identityProvider dynamic.Interface,
	coreProvider dynamic.Interface,
) (bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	changed, err := removeAPIExportResource(
		ctx,
		coreProvider,
		coreAPIBindingName,
		identityProviderResourceGroup,
		identityProviderResourceName,
	)
	if err != nil {
		return false, fmt.Errorf("remove conflicting identity-provider resource from core APIExport: %w", err)
	}
	if changed {
		log.Info().
			Str("subroutine", platformRecoverySubroutineName).
			Str("platformRestore", pr.Name).
			Str("apiExport", coreAPIBindingName).
			Str("resource", identityProviderResourceName+"."+identityProviderResourceGroup).
			Msg("removed conflicting identity-provider resource from core APIExport")
		return false, nil
	}

	changed, err = removeAPIBindingAnnotation(ctx, consumer, coreAPIBindingName, staleCoreBindingResetAnnotation)
	if err != nil {
		return false, fmt.Errorf("remove stale core APIBinding recovery annotation: %w", err)
	}
	if changed {
		return false, nil
	}

	binding, err := consumer.Resource(apiBindingGVR).Get(ctx, systemAPIBindingName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get %s APIBinding while validating bound resources: %w", systemAPIBindingName, err)
	}

	boundResources, found, err := unstructured.NestedSlice(binding.Object, "status", "boundResources")
	if err != nil {
		return false, fmt.Errorf("read %s APIBinding bound resources: %w", systemAPIBindingName, err)
	}
	if found {
		for _, item := range boundResources {
			resource, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if resource["group"] == identityProviderResourceGroup && resource["resource"] == identityProviderResourceName {
				return true, nil
			}
		}
	}

	export, err := identityProvider.Resource(apiExportGVR).Get(ctx, systemAPIBindingName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get %s APIExport: %w", systemAPIBindingName, err)
	}
	resources, found, err := unstructured.NestedSlice(export.Object, "spec", "resources")
	if err != nil {
		return false, fmt.Errorf("read %s APIExport resources: %w", systemAPIBindingName, err)
	}
	if !found || !containsAPIExportResource(resources, identityProviderResourceGroup, identityProviderResourceName) {
		schemas, err := identityProvider.Resource(apiResourceSchemaGVR).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, fmt.Errorf("list APIResourceSchemas for system APIExport repair: %w", err)
		}
		var schemaName string
		for _, item := range schemas.Items {
			group, _, _ := unstructured.NestedString(item.Object, "spec", "group")
			plural, _, _ := unstructured.NestedString(item.Object, "spec", "names", "plural")
			if group == identityProviderResourceGroup && plural == identityProviderResourceName {
				schemaName = item.GetName()
				break
			}
		}
		if schemaName == "" {
			return false, fmt.Errorf("waiting for APIResourceSchema for %s/%s", identityProviderResourceGroup, identityProviderResourceName)
		}
		resources = append(resources, map[string]any{
			"group":   identityProviderResourceGroup,
			"name":    identityProviderResourceName,
			"schema":  schemaName,
			"storage": map[string]any{"crd": map[string]any{}},
		})
		if err := unstructured.SetNestedSlice(export.Object, resources, "spec", "resources"); err != nil {
			return false, fmt.Errorf("set system APIExport identityproviderconfiguration resource: %w", err)
		}
		if _, err := identityProvider.Resource(apiExportGVR).Update(ctx, export, metav1.UpdateOptions{}); err != nil {
			return false, fmt.Errorf("update system APIExport with identityproviderconfigurations: %w", err)
		}
		log.Info().
			Str("subroutine", platformRecoverySubroutineName).
			Str("platformRestore", pr.Name).
			Str("apiExport", systemAPIBindingName).
			Str("schema", schemaName).
			Msg("repaired system APIExport resource list for Portal identity configuration")
		return false, nil
	}

	annotations := binding.GetAnnotations()
	if annotations != nil && annotations[identityBindingStatusResetAnnotation] == string(pr.UID) {
		return false, nil
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[identityBindingStatusResetAnnotation] = string(pr.UID)
	binding.SetAnnotations(annotations)
	updatedBinding, err := consumer.Resource(apiBindingGVR).Update(ctx, binding, metav1.UpdateOptions{})
	if err != nil {
		return false, fmt.Errorf("mark stale %s APIBinding for status repair: %w", systemAPIBindingName, err)
	}
	// Do not delete the APIBinding: its finalizer can delete restored bound
	// objects. Clearing status is safe because status is derived exclusively by
	// KCP's binding controller.
	updatedBinding.Object["status"] = map[string]any{}
	if _, err := consumer.Resource(apiBindingGVR).UpdateStatus(ctx, updatedBinding, metav1.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("clear stale %s APIBinding status: %w", systemAPIBindingName, err)
	}
	log.Info().
		Str("subroutine", platformRecoverySubroutineName).
		Str("platformRestore", pr.Name).
		Str("apiBinding", systemAPIBindingName).
		Msg("cleared stale APIBinding status so KCP recomputes bound resources")
	return false, nil
}

func apiBindingExportPath(ctx context.Context, client dynamic.Interface, name string) (string, error) {
	binding, err := client.Resource(apiBindingGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get %s APIBinding: %w", name, err)
	}
	exportPath, found, err := unstructured.NestedString(binding.Object, "spec", "reference", "export", "path")
	if err != nil {
		return "", fmt.Errorf("read %s APIBinding export path: %w", name, err)
	}
	if !found || exportPath == "" {
		return "", fmt.Errorf("%s APIBinding has no APIExport path", name)
	}
	return exportPath, nil
}

func removeAPIExportResource(
	ctx context.Context,
	client dynamic.Interface,
	exportName, group, name string,
) (bool, error) {
	export, err := client.Resource(apiExportGVR).Get(ctx, exportName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	resources, found, err := unstructured.NestedSlice(export.Object, "spec", "resources")
	if err != nil || !found {
		return false, err
	}
	filtered := make([]any, 0, len(resources))
	for _, item := range resources {
		resource, ok := item.(map[string]any)
		if ok && resource["group"] == group && resource["name"] == name {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == len(resources) {
		return false, nil
	}
	if err := unstructured.SetNestedSlice(export.Object, filtered, "spec", "resources"); err != nil {
		return false, err
	}
	_, err = client.Resource(apiExportGVR).Update(ctx, export, metav1.UpdateOptions{})
	return err == nil, err
}

func removeAPIBindingAnnotation(
	ctx context.Context,
	client dynamic.Interface,
	bindingName, annotation string,
) (bool, error) {
	binding, err := client.Resource(apiBindingGVR).Get(ctx, bindingName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	annotations := binding.GetAnnotations()
	if annotations == nil || annotations[annotation] == "" {
		return false, nil
	}
	delete(annotations, annotation)
	binding.SetAnnotations(annotations)
	_, err = client.Resource(apiBindingGVR).Update(ctx, binding, metav1.UpdateOptions{})
	return err == nil, err
}

func containsAPIExportResource(resources []any, group, name string) bool {
	for _, item := range resources {
		resource, ok := item.(map[string]any)
		if ok && resource["group"] == group && resource["name"] == name {
			return true
		}
	}
	return false
}

func (p *PlatformRecoverySubroutine) ensureKCPAPIExportProviderReaders(ctx context.Context, consumer dynamic.Interface) (bool, error) {
	bindings := consumer.Resource(apiBindingGVR)
	changed := false
	for _, name := range []string{"core.platform-mesh.io", "system.platform-mesh.io"} {
		binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("get APIBinding %s while finding provider workspace: %w", name, err)
		}
		exportPath, found, err := unstructured.NestedString(binding.Object, "spec", "reference", "export", "path")
		if err != nil {
			return false, fmt.Errorf("read APIBinding %s provider workspace: %w", name, err)
		}
		if !found || exportPath == "" {
			return false, fmt.Errorf("APIBinding %s has no provider APIExport path", name)
		}
		provider, err := p.kcpRecoveryDynamicClient(ctx, exportPath)
		if err != nil {
			return false, fmt.Errorf("create provider client for APIBinding %s at %s: %w", name, exportPath, err)
		}
		updated, err := ensureKCPVirtualWorkspaceSchemaReader(ctx, provider)
		if err != nil {
			return false, fmt.Errorf("ensure provider reader for APIBinding %s at %s: %w", name, exportPath, err)
		}
		changed = changed || updated
	}
	return !changed, nil
}

func ensureKCPVirtualWorkspaceSchemaReader(ctx context.Context, client dynamic.Interface) (bool, error) {
	const roleName = "platform-restore-virtual-workspace-schema-reader"
	role := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRole",
		"metadata":   map[string]any{"name": roleName},
		"rules": []any{map[string]any{
			"apiGroups": []any{"apis.kcp.io"},
			// The virtual-workspaces path-aware provider must watch the
			// endpoint slice before it can discover the APIExport-backed
			// cluster that serves the schema.
			"resources": []any{"apiresourceschemas", "apibindings", "apiexports", "apiexportendpointslices"},
			"verbs":     []any{"get", "list", "watch"},
		}, map[string]any{
			// client-go's REST mapper discovers these API groups before it can
			// create the endpoint-slice cache.
			"nonResourceURLs": []any{"/api", "/api/*", "/apis", "/apis/*"},
			"verbs":           []any{"get"},
		}, map[string]any{
			// KCP's workspace-content authorizer evaluates this synthetic
			// permission before the ordinary in-workspace RBAC rules. Without it,
			// front-proxy rejects the request before the discovery grant above is
			// considered, even though the same identity can use the physical shard.
			"nonResourceURLs": []any{"/"},
			"verbs":           []any{"access"},
		}, map[string]any{
			// A front-proxy request for a core.platform-mesh.io resource is
			// served by the APIExport virtual workspace. Before it delegates to
			// the resource RBAC below, KCP requires access to the provider
			// APIExport's content subresource. Without this rule it returns
			// NoOpinion even though the caller can read the same object directly
			// from the physical provider workspace.
			"apiGroups": []any{"apis.kcp.io"},
			"resources": []any{"apiexports/content"},
			"verbs":     []any{"get", "list", "watch"},
		}, map[string]any{
			// Portal uses the same destination credential through the
			// front-proxy to bootstrap its identity configuration and client
			// secret. The RBAC grant is intentionally limited to those reads.
			"apiGroups": []any{"core.platform-mesh.io"},
			"resources": []any{"identityproviderconfigurations"},
			"verbs":     []any{"get", "list", "watch"},
		}, map[string]any{
			"apiGroups": []any{""},
			"resources": []any{"secrets"},
			"verbs":     []any{"get"},
		}},
	}}
	binding := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRoleBinding",
		"metadata":   map[string]any{"name": roleName},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "ClusterRole",
			"name":     roleName,
		},
		"subjects": []any{
			map[string]any{"kind": "User", "name": "root", "apiGroup": "rbac.authorization.k8s.io"},
			// APIExport virtual workspaces evaluate the caller with the bound
			// identity. The endpoint-slice provider uses this path, so grant the
			// restore bootstrap identity in both its direct and bound forms.
			map[string]any{"kind": "User", "name": "apis.kcp.io:binding:root", "apiGroup": "rbac.authorization.k8s.io"},
			map[string]any{"kind": "User", "name": "kcp-admin", "apiGroup": "rbac.authorization.k8s.io"},
			map[string]any{"kind": "User", "name": "apis.kcp.io:binding:kcp-admin", "apiGroup": "rbac.authorization.k8s.io"},
			// kubeconfig-kcp-admin authenticates as this group. KCP prefixes
			// groups as well as users when it evaluates an APIExport binding,
			// so retain both forms for the provider virtual workspace.
			map[string]any{"kind": "Group", "name": "system:kcp:admin", "apiGroup": "rbac.authorization.k8s.io"},
			map[string]any{"kind": "Group", "name": "apis.kcp.io:binding:system:kcp:admin", "apiGroup": "rbac.authorization.k8s.io"},
			map[string]any{"kind": "Group", "name": "system:masters", "apiGroup": "rbac.authorization.k8s.io"},
			map[string]any{"kind": "Group", "name": "apis.kcp.io:binding:system:masters", "apiGroup": "rbac.authorization.k8s.io"},
		},
	}}
	return ensureKCPRoleAndBinding(ctx, client, role, binding, kcpRBACReconcile)
}

// ensureKCPAuthorizationConfiguration removes a Kubernetes API-server flag
// that KCP v0.32 does not implement. KCP's RBAC chain is intrinsic; its
// supported external authorization configuration is rendered by kcp-operator
// from spec.authorization.webhook. Leaving --authorization-mode in extraArgs
// makes every regenerated KCP pod exit before it can serve requests.
func (p *PlatformRecoverySubroutine) ensureKCPAuthorizationConfiguration(ctx context.Context) (bool, error) {
	changed := false
	for _, gvk := range kcpOperatorResources {
		objects := &unstructured.UnstructuredList{}
		objects.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
		if err := p.client.List(ctx, objects, ctrlruntimeclient.InNamespace(platformMeshNamespace)); err != nil {
			return false, fmt.Errorf("list %s resources: %w", gvk.Kind, err)
		}
		if len(objects.Items) == 0 {
			return false, fmt.Errorf("no %s resources found in %s", gvk.Kind, platformMeshNamespace)
		}

		for index := range objects.Items {
			object := &objects.Items[index]
			base := object.DeepCopy()
			args, found, err := unstructured.NestedStringSlice(object.Object, "spec", "extraArgs")
			if err != nil {
				return false, fmt.Errorf("read %s/%s spec.extraArgs: %w", gvk.Kind, object.GetName(), err)
			}
			if !found {
				args = nil
			}

			updatedArgs := make([]string, 0, len(args))
			for _, arg := range args {
				if strings.HasPrefix(arg, "--authorization-mode=") {
					continue
				}
				updatedArgs = append(updatedArgs, arg)
			}
			if reflect.DeepEqual(args, updatedArgs) {
				continue
			}
			if err := unstructured.SetNestedStringSlice(object.Object, updatedArgs, "spec", "extraArgs"); err != nil {
				return false, fmt.Errorf("set %s/%s spec.extraArgs: %w", gvk.Kind, object.GetName(), err)
			}
			if err := p.client.Patch(ctx, object, ctrlruntimeclient.MergeFrom(base)); err != nil {
				return false, fmt.Errorf("remove unsupported authorization mode from %s/%s: %w", gvk.Kind, object.GetName(), err)
			}
			changed = true
		}
	}
	if changed {
		return false, nil
	}

	for _, workload := range kcpWebhookConsumers {
		var deployment appsv1.Deployment
		if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: workload.Namespace, Name: workload.Name}, &deployment); err != nil {
			return false, fmt.Errorf("get KCP Deployment %s/%s: %w", workload.Namespace, workload.Name, err)
		}
		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			return false, fmt.Errorf("KCP Deployment %s/%s has no containers", workload.Namespace, workload.Name)
		}
		webhookConfigured := false
		for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
			if strings.HasPrefix(arg, "--authorization-mode=") {
				return false, nil
			}
			if strings.HasPrefix(arg, "--authorization-webhook-config-file=") {
				webhookConfigured = true
			}
		}
		if !webhookConfigured {
			return false, nil
		}
	}

	return workloadsReady(ctx, p.client, kcpWebhookConsumers)
}

// ensureKCPFrontProxyReady restarts front-proxy after the final KCP rollout.
// Its workspace-authentication informers can retain a negative discovery
// result when KCP is temporarily unavailable; those reflectors then continue
// returning NoOpinion for bound resources even after the shard is healthy.
func (p *PlatformRecoverySubroutine) ensureKCPFrontProxyReady(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	frontProxy := platformDeployment("frontproxy-front-proxy")
	changed, err := restartDeployment(
		ctx,
		p.client,
		frontProxy,
		frontProxyRestartAnnotation,
		string(pr.UID),
	)
	if err != nil {
		return false, err
	}
	if changed {
		return false, nil
	}
	return frontProxy.ready(ctx, p.client)
}

const (
	kcpApplicationTokenBootstrapRoleName    = "platform-restore-application-token-bootstrap"
	kcpApplicationTokenBootstrapBindingName = "platform-restore-application-token-bootstrap"
	rebacEndpointDiscoveryRoleName          = "platform-restore-rebac-endpoint-discovery"
	rebacEndpointDiscoveryBindingName       = "platform-restore-rebac-endpoint-discovery"
)

// ensureKCPApplicationTokenBootstrapAccess grants the destination root-client
// only the permissions required to create replacement ServiceAccount tokens.
// The source platform's KCP RBAC is restored by Velero and cannot authorize
// that destination identity until those replacement credentials exist.
func (p *PlatformRecoverySubroutine) ensureKCPApplicationTokenBootstrapAccess(ctx context.Context) (bool, error) {
	changed := false
	for _, logicalPath := range []string{platformSystemPath, orgsLogicalClusterPath} {
		client, err := p.kcpRecoveryDynamicClient(ctx, logicalPath)
		if err != nil {
			return false, err
		}
		role := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRole",
			"metadata":   map[string]any{"name": kcpApplicationTokenBootstrapRoleName},
			"rules": []any{
				map[string]any{
					"apiGroups": []any{""},
					"resources": []any{"namespaces"},
					"verbs":     []any{"get", "list", "create"},
				},
				map[string]any{
					"apiGroups": []any{""},
					"resources": []any{"serviceaccounts"},
					"verbs":     []any{"get"},
				},
				map[string]any{
					"apiGroups": []any{""},
					"resources": []any{"serviceaccounts/token"},
					"verbs":     []any{"create"},
				},
			},
		}}
		binding := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRoleBinding",
			"metadata":   map[string]any{"name": kcpApplicationTokenBootstrapBindingName},
			"roleRef": map[string]any{
				"apiGroup": "rbac.authorization.k8s.io",
				"kind":     "ClusterRole",
				"name":     kcpApplicationTokenBootstrapRoleName,
			},
			"subjects": []any{map[string]any{
				"kind": "User", "name": "root", "apiGroup": "rbac.authorization.k8s.io",
			}},
		}}
		created, err := ensureKCPRoleAndBinding(ctx, client, role, binding, kcpRBACCreateOnly)
		if err != nil {
			return false, fmt.Errorf("grant token bootstrap access in %s: %w", logicalPath, err)
		}
		changed = changed || created
	}
	return changed, nil
}

// ensureReBACEndpointDiscoveryAccess grants ReBAC's long-lived application
// identity the KCP permission it needs after the restore bootstrap credential
// is replaced with a service-account token. The path-aware provider watches
// APIExportEndpointSlices in the core APIExport provider workspace to discover
// the logical clusters it must authorize. Without this binding, it cannot
// register root:orgs and answers NoOpinion to every request for the
// organizations workspace.
func (p *PlatformRecoverySubroutine) ensureReBACEndpointDiscoveryAccess(ctx context.Context) (bool, error) {
	client, err := p.kcpRecoveryDynamicClient(ctx, platformSystemPath)
	if err != nil {
		return false, err
	}

	role := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRole",
		"metadata":   map[string]any{"name": rebacEndpointDiscoveryRoleName},
		"rules": []any{map[string]any{
			"apiGroups": []any{"apis.kcp.io"},
			"resources": []any{"apiexportendpointslices"},
			"verbs":     []any{"get", "list", "watch"},
		}, map[string]any{
			"nonResourceURLs": []any{"/api", "/api/*", "/apis", "/apis/*"},
			"verbs":           []any{"get"},
		}},
	}}
	binding := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRoleBinding",
		"metadata":   map[string]any{"name": rebacEndpointDiscoveryBindingName},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "ClusterRole",
			"name":     rebacEndpointDiscoveryRoleName,
		},
		"subjects": []any{map[string]any{
			"kind":      "ServiceAccount",
			"name":      applicationTokenRecipes[1].ServiceAccount,
			"namespace": metav1.NamespaceDefault,
		}},
	}}

	changed, err := ensureKCPRoleAndBinding(ctx, client, role, binding, kcpRBACReconcile)
	if err != nil {
		return false, fmt.Errorf("grant ReBAC endpoint discovery access in %s: %w", platformSystemPath, err)
	}
	return changed, nil
}

// ensureKCPRoleAndBinding creates a restore-owned RBAC pair. When reconcile
// is true it also repairs a stale role or binding from an earlier restore.
func ensureKCPRoleAndBinding(ctx context.Context, client dynamic.Interface, role, binding *unstructured.Unstructured, reconcile bool) (bool, error) {
	changed := false
	roles := client.Resource(kcpClusterRoleGVR)
	if !reconcile {
		if _, err := roles.Create(ctx, role, metav1.CreateOptions{}); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return false, fmt.Errorf("create ClusterRole %s: %w", role.GetName(), err)
			}
		} else {
			changed = true
		}
		bindings := client.Resource(kcpClusterRoleBindingGVR)
		if _, err := bindings.Create(ctx, binding, metav1.CreateOptions{}); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return false, fmt.Errorf("create ClusterRoleBinding %s: %w", binding.GetName(), err)
			}
		} else {
			changed = true
		}
		return changed, nil
	}
	existingRole, err := roles.Get(ctx, role.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := roles.Create(ctx, role, metav1.CreateOptions{}); err != nil {
			return false, fmt.Errorf("create ClusterRole %s: %w", role.GetName(), err)
		}
		changed = true
	} else if err != nil {
		return false, fmt.Errorf("get ClusterRole %s: %w", role.GetName(), err)
	} else if !reflect.DeepEqual(existingRole.Object["rules"], role.Object["rules"]) {
		role.SetResourceVersion(existingRole.GetResourceVersion())
		if _, err := roles.Update(ctx, role, metav1.UpdateOptions{}); err != nil {
			return false, fmt.Errorf("update ClusterRole %s: %w", role.GetName(), err)
		}
		changed = true
	}

	bindings := client.Resource(kcpClusterRoleBindingGVR)
	existingBinding, err := bindings.Get(ctx, binding.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := bindings.Create(ctx, binding, metav1.CreateOptions{}); err != nil {
			return false, fmt.Errorf("create ClusterRoleBinding %s: %w", binding.GetName(), err)
		}
		changed = true
	} else if err != nil {
		return false, fmt.Errorf("get ClusterRoleBinding %s: %w", binding.GetName(), err)
	} else if !reflect.DeepEqual(existingBinding.Object["roleRef"], binding.Object["roleRef"]) ||
		!reflect.DeepEqual(existingBinding.Object["subjects"], binding.Object["subjects"]) {
		binding.SetResourceVersion(existingBinding.GetResourceVersion())
		if _, err := bindings.Update(ctx, binding, metav1.UpdateOptions{}); err != nil {
			return false, fmt.Errorf("update ClusterRoleBinding %s: %w", binding.GetName(), err)
		}
		changed = true
	}

	return changed, nil
}

func (p *PlatformRecoverySubroutine) ensureReBACWebhookTrust(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	var servingSecret corev1.Secret
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: platformMeshNamespace,
		Name:      rebacServingSecretName,
	}, &servingSecret); err != nil {
		return false, fmt.Errorf("waiting for ReBAC serving Secret: %w", err)
	}

	caPEM := servingSecret.Data["ca.crt"]
	if len(caPEM) == 0 {
		return false, fmt.Errorf(
			"waiting for %s/%s to contain ca.crt",
			platformMeshNamespace,
			rebacServingSecretName,
		)
	}

	var webhookSecret corev1.Secret
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: platformMeshNamespace,
		Name:      kcpWebhookSecretName,
	}, &webhookSecret); err != nil {
		return false, fmt.Errorf("waiting for KCP webhook Secret: %w", err)
	}

	raw := webhookSecret.Data["kubeconfig"]
	if len(raw) == 0 {
		return false, fmt.Errorf(
			"waiting for %s/%s to contain data.kubeconfig",
			platformMeshNamespace,
			kcpWebhookSecretName,
		)
	}

	cfg, err := clientcmd.Load(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s kubeconfig: %w", kcpWebhookSecretName, err)
	}

	found := false
	changed := false
	for clusterName, cluster := range cfg.Clusters {
		u, err := url.Parse(cluster.Server)
		if err != nil {
			return false, fmt.Errorf(
				"parse webhook server URL %q: %w",
				cluster.Server,
				err,
			)
		}

		if u.Hostname() != rebacWebhookHost {
			continue
		}

		found = true
		if bytes.Equal(cluster.CertificateAuthorityData, caPEM) &&
			cluster.CertificateAuthority == "" {
			log.Info().
				Str("cluster", clusterName).
				Str("host", rebacWebhookHost).
				Msg("ReBAC webhook CA is already current")
			continue
		}

		cluster.CertificateAuthority = ""
		cluster.CertificateAuthorityData = caPEM
		changed = true

		log.Info().
			Str("cluster", clusterName).
			Str("host", rebacWebhookHost).
			Msg("updating ReBAC webhook CA")
	}

	if !found {
		return false, fmt.Errorf(
			"no cluster in %s points to %s",
			kcpWebhookSecretName,
			rebacWebhookHost,
		)
	}
	if !changed {
		return false, nil
	}

	updated, err := clientcmd.Write(*cfg)
	if err != nil {
		return false, fmt.Errorf("write repaired webhook kubeconfig: %w", err)
	}

	patchBase := webhookSecret.DeepCopy()
	webhookSecret.Data["kubeconfig"] = updated
	if err := p.client.Patch(
		ctx,
		&webhookSecret,
		ctrlruntimeclient.MergeFrom(patchBase),
	); err != nil {
		return false, fmt.Errorf(
			"patch %s/%s: %w",
			platformMeshNamespace,
			kcpWebhookSecretName,
			err,
		)
	}

	for _, workload := range kcpWebhookConsumers {
		restarted, err := restartDeployment(
			ctx,
			p.client,
			workload,
			webhookRestartAnnotation,
			string(pr.UID),
		)
		if err != nil {
			return false, err
		}

		log.Info().
			Str("namespace", workload.Namespace).
			Str("workload", workload.Name).
			Bool("restarted", restarted).
			Msg("processed KCP webhook consumer restart")
	}

	return true, nil
}

func (p *PlatformRecoverySubroutine) ensureApplicationTokens(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	changed := false
	for _, recipe := range applicationTokenRecipes {
		log.Info().
			Str("secret", recipe.SecretName).
			Str("logicalPath", recipe.LogicalPath).
			Str("serviceAccount", recipe.ServiceAccount).
			Msg("processing application credential")

		updated, err := p.refreshToken(ctx, pr, recipe)
		if err != nil {
			return changed, fmt.Errorf(
				"refresh application credential %s: %w",
				recipe.SecretName,
				err,
			)
		}

		changed = changed || updated
	}

	return changed, nil
}

func (p *PlatformRecoverySubroutine) refreshToken(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
	recipe tokenRecipe,
) (bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	var secret corev1.Secret
	err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: platformMeshNamespace,
		Name:      recipe.SecretName,
	}, &secret)
	if apierrors.IsNotFound(err) {
		log.Info().
			Str("namespace", platformMeshNamespace).
			Str("secret", recipe.SecretName).
			Msg("application kubeconfig Secret does not exist; skipping")

		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf(
			"get %s/%s: %w",
			platformMeshNamespace,
			recipe.SecretName,
			err,
		)
	}

	raw := secret.Data["kubeconfig"]
	if len(raw) == 0 {
		return false, fmt.Errorf(
			"Secret %s/%s has no data.kubeconfig",
			platformMeshNamespace,
			recipe.SecretName,
		)
	}

	// Use the destination shard and physical logical-cluster endpoint. The
	// restored front-proxy kubeconfig can still carry source-platform routing
	// and does not reliably observe the temporary recovery RBAC binding.
	restConfig, err := p.kcpRecoveryRESTConfig(ctx, recipe.LogicalPath)
	if err != nil {
		return false, err
	}
	targetServer := restConfig.Host
	targetCurrent, err := kubeconfigTargetsServer(raw, targetServer)
	if err != nil {
		return false, fmt.Errorf(
			"inspect %s kubeconfig server: %w",
			recipe.SecretName,
			err,
		)
	}

	if secret.Annotations != nil &&
		secret.Annotations[tokenRefreshAnnotation] == string(pr.UID) &&
		targetCurrent {
		log.Info().
			Str("secret", recipe.SecretName).
			Msg("application credential is already refreshed for this restore")

		return false, nil
	}

	log.Info().
		Str("secret", recipe.SecretName).
		Str("logicalPath", recipe.LogicalPath).
		Str("serviceAccount", recipe.ServiceAccount).
		Str("host", restConfig.Host).
		Msg("requesting KCP service-account token")

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return false, fmt.Errorf(
			"build KCP client for %s: %w",
			recipe.LogicalPath,
			err,
		)
	}

	// ServiceAccounts live in the logical cluster's default namespace. KCP can
	// make a restored logical cluster reachable before that namespace exists,
	// so ensure the standard namespace before issuing a TokenRequest.
	_, err = clientset.CoreV1().Namespaces().Get(ctx, metav1.NamespaceDefault, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, createErr := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: metav1.NamespaceDefault},
		}, metav1.CreateOptions{})
		if createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return false, fmt.Errorf(
				"create namespace %q in %s before requesting token for %s: %w",
				metav1.NamespaceDefault,
				recipe.LogicalPath,
				recipe.ServiceAccount,
				createErr,
			)
		}

		log.Info().
			Str("namespace", metav1.NamespaceDefault).
			Str("logicalPath", recipe.LogicalPath).
			Msg("created KCP namespace required for service-account token")
		err = nil
	}
	if err != nil {
		return false, fmt.Errorf(
			"get namespace %q in %s: %w",
			metav1.NamespaceDefault,
			recipe.LogicalPath,
			err,
		)
	}

	_, err = clientset.CoreV1().ServiceAccounts(metav1.NamespaceDefault).Get(
		ctx,
		recipe.ServiceAccount,
		metav1.GetOptions{},
	)
	if apierrors.IsNotFound(err) {
		if recipe.FallbackToAdmin {
			log.Warn().
				Str("secret", recipe.SecretName).
				Str("serviceAccount", recipe.ServiceAccount).
				Str("logicalPath", recipe.LogicalPath).
				Msg("service account is absent; retaining destination bootstrap credential")
			return false, nil
		}
		return false, fmt.Errorf(
			"waiting for service account %s in namespace %q in %s before requesting token",
			recipe.ServiceAccount,
			metav1.NamespaceDefault,
			recipe.LogicalPath,
		)
	}
	if err != nil {
		return false, fmt.Errorf(
			"get service account %s in namespace %q in %s: %w",
			recipe.ServiceAccount,
			metav1.NamespaceDefault,
			recipe.LogicalPath,
			err,
		)
	}

	expiration := int64(365 * 24 * 60 * 60)

	tokenCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	request, err := clientset.CoreV1().
		ServiceAccounts("default").
		CreateToken(
			tokenCtx,
			recipe.ServiceAccount,
			&authenticationv1.TokenRequest{
				Spec: authenticationv1.TokenRequestSpec{
					Audiences: []string{
						"https://kcp.default.svc",
					},
					ExpirationSeconds: &expiration,
				},
			},
			metav1.CreateOptions{},
		)
	if err != nil {
		if recipe.FallbackToAdmin {
			log.Warn().
				Err(err).
				Str("secret", recipe.SecretName).
				Str("logicalPath", recipe.LogicalPath).
				Msg("service-account token request failed; using admin credential fallback")

			return p.patchSecretWithAdminCredential(ctx, pr, recipe, &secret)
		}

		return false, fmt.Errorf(
			"request token for %s in %s: %w",
			recipe.ServiceAccount,
			recipe.LogicalPath,
			err,
		)
	}
	if request.Status.Token == "" {
		return false, fmt.Errorf(
			"token request for %s returned an empty token",
			recipe.ServiceAccount,
		)
	}

	log.Info().
		Str("secret", recipe.SecretName).
		Str("logicalPath", recipe.LogicalPath).
		Str(
			"expiresAt",
			request.Status.ExpirationTimestamp.Time.UTC().Format(time.RFC3339),
		).
		Msg("KCP service-account token created")

	cfg, err := clientcmd.Load(raw)
	if err != nil {
		return false, fmt.Errorf(
			"parse %s kubeconfig: %w",
			recipe.SecretName,
			err,
		)
	}

	authInfoName, authInfo, err := currentAuthInfoEntry(cfg)
	if err != nil {
		return false, fmt.Errorf(
			"select auth info from %s: %w",
			recipe.SecretName,
			err,
		)
	}

	log.Info().
		Str("secret", recipe.SecretName).
		Str("authInfo", authInfoName).
		Int("contexts", len(cfg.Contexts)).
		Int("authInfos", len(cfg.AuthInfos)).
		Msg("selected kubeconfig auth info")

	authInfo.Token = request.Status.Token
	authInfo.TokenFile = ""
	authInfo.ClientCertificate = ""
	authInfo.ClientCertificateData = nil
	authInfo.ClientKey = ""
	authInfo.ClientKeyData = nil
	authInfo.Exec = nil
	authInfo.AuthProvider = nil

	clusterName, cluster, err := currentClusterEntry(cfg)
	if err != nil {
		return false, fmt.Errorf(
			"select cluster from %s: %w",
			recipe.SecretName,
			err,
		)
	}
	cluster.Server = targetServer
	cluster.CertificateAuthority = ""
	cluster.CertificateAuthorityData = restConfig.CAData
	cluster.InsecureSkipTLSVerify = false

	log.Info().
		Str("secret", recipe.SecretName).
		Str("cluster", clusterName).
		Str("server", targetServer).
		Msg("selected kubeconfig cluster server")

	updated, err := clientcmd.Write(*cfg)
	if err != nil {
		return false, fmt.Errorf(
			"write %s kubeconfig: %w",
			recipe.SecretName,
			err,
		)
	}

	patchBase := secret.DeepCopy()
	secret.Data["kubeconfig"] = updated
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations[tokenRefreshAnnotation] = string(pr.UID)

	log.Info().
		Str("namespace", platformMeshNamespace).
		Str("secret", recipe.SecretName).
		Msg("patching application kubeconfig Secret")

	if err := p.client.Patch(
		ctx,
		&secret,
		ctrlruntimeclient.MergeFrom(patchBase),
	); err != nil {
		return false, fmt.Errorf(
			"patch %s/%s: %w",
			platformMeshNamespace,
			recipe.SecretName,
			err,
		)
	}

	log.Info().
		Str("namespace", platformMeshNamespace).
		Str("secret", recipe.SecretName).
		Msg("application kubeconfig Secret patched")

	restarted, err := restartDeployment(
		ctx,
		p.client,
		recipe.Workload,
		tokenRestartAnnotation,
		string(pr.UID),
	)
	if err != nil {
		return false, fmt.Errorf(
			"restart %s/%s after refreshing %s: %w",
			recipe.Workload.Namespace,
			recipe.Workload.Name,
			recipe.SecretName,
			err,
		)
	}

	log.Info().
		Str("namespace", recipe.Workload.Namespace).
		Str("workload", recipe.Workload.Name).
		Str("secret", recipe.SecretName).
		Bool("restarted", restarted).
		Msg("processed application workload restart")

	return true, nil
}

func (p *PlatformRecoverySubroutine) patchSecretWithAdminCredential(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
	recipe tokenRecipe,
	secret *corev1.Secret,
) (bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	var adminSecret corev1.Secret
	if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: platformMeshNamespace,
		Name:      kcpAdminSecretName,
	}, &adminSecret); err != nil {
		return false, fmt.Errorf(
			"get admin kubeconfig Secret %s/%s: %w",
			platformMeshNamespace,
			kcpAdminSecretName,
			err,
		)
	}

	adminRaw := adminSecret.Data["kubeconfig"]
	if len(adminRaw) == 0 {
		return false, fmt.Errorf(
			"Secret %s/%s has no data.kubeconfig",
			platformMeshNamespace,
			kcpAdminSecretName,
		)
	}

	adminConfig, err := clientcmd.Load(adminRaw)
	if err != nil {
		return false, fmt.Errorf(
			"parse %s kubeconfig: %w",
			kcpAdminSecretName,
			err,
		)
	}

	adminAuthInfoName, adminAuthInfo, err := currentAuthInfoEntry(adminConfig)
	if err != nil {
		return false, fmt.Errorf(
			"select auth info from %s: %w",
			kcpAdminSecretName,
			err,
		)
	}

	targetRaw := secret.Data["kubeconfig"]
	if len(targetRaw) == 0 {
		return false, fmt.Errorf(
			"Secret %s/%s has no data.kubeconfig",
			platformMeshNamespace,
			recipe.SecretName,
		)
	}

	targetConfig, err := clientcmd.Load(targetRaw)
	if err != nil {
		return false, fmt.Errorf(
			"parse %s kubeconfig: %w",
			recipe.SecretName,
			err,
		)
	}

	targetAuthInfoName, targetAuthInfo, err := currentAuthInfoEntry(targetConfig)
	if err != nil {
		return false, fmt.Errorf(
			"select auth info from %s: %w",
			recipe.SecretName,
			err,
		)
	}

	log.Warn().
		Str("secret", recipe.SecretName).
		Str("adminAuthInfo", adminAuthInfoName).
		Str("targetAuthInfo", targetAuthInfoName).
		Msg("patching application kubeconfig with admin credential fallback")

	*targetAuthInfo = *adminAuthInfo

	targetClusterName, targetCluster, err := currentClusterEntry(targetConfig)
	if err != nil {
		return false, fmt.Errorf(
			"select cluster from %s: %w",
			recipe.SecretName,
			err,
		)
	}
	targetCluster.Server = kcpLogicalClusterURL(recipe.LogicalPath)

	log.Warn().
		Str("secret", recipe.SecretName).
		Str("targetCluster", targetClusterName).
		Str("server", targetCluster.Server).
		Msg("selected admin fallback kubeconfig cluster server")

	updated, err := clientcmd.Write(*targetConfig)
	if err != nil {
		return false, fmt.Errorf(
			"write %s kubeconfig with admin credential: %w",
			recipe.SecretName,
			err,
		)
	}

	patchBase := secret.DeepCopy()
	secret.Data["kubeconfig"] = updated
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations[tokenRefreshAnnotation] = string(pr.UID)

	if err := p.client.Patch(
		ctx,
		secret,
		ctrlruntimeclient.MergeFrom(patchBase),
	); err != nil {
		return false, fmt.Errorf(
			"patch %s/%s with admin credential: %w",
			platformMeshNamespace,
			recipe.SecretName,
			err,
		)
	}

	restarted, err := restartDeployment(
		ctx,
		p.client,
		recipe.Workload,
		tokenRestartAnnotation,
		string(pr.UID),
	)
	if err != nil {
		return false, fmt.Errorf(
			"restart %s/%s after admin credential fallback: %w",
			recipe.Workload.Namespace,
			recipe.Workload.Name,
			err,
		)
	}

	log.Warn().
		Str("secret", recipe.SecretName).
		Str("namespace", recipe.Workload.Namespace).
		Str("workload", recipe.Workload.Name).
		Bool("restarted", restarted).
		Msg("admin credential fallback applied")

	return true, nil
}

func kcpLogicalClusterURL(logicalPath string) string {
	baseURL := os.Getenv("KCP_BASE_URL")
	if baseURL == "" {
		baseURL = kcpBaseURLDefault
	}

	return fmt.Sprintf(
		"%s/clusters/%s",
		strings.TrimRight(baseURL, "/"),
		logicalPath,
	)
}

func kubeconfigTargetsServer(raw []byte, expectedServer string) (bool, error) {
	cfg, err := clientcmd.Load(raw)
	if err != nil {
		return false, err
	}

	_, cluster, err := currentClusterEntry(cfg)
	if err != nil {
		return false, err
	}

	return strings.TrimRight(cluster.Server, "/") ==
		strings.TrimRight(expectedServer, "/"), nil
}

func currentClusterEntry(
	cfg *clientcmdapi.Config,
) (string, *clientcmdapi.Cluster, error) {
	if cfg.CurrentContext != "" {
		contextConfig := cfg.Contexts[cfg.CurrentContext]
		if contextConfig != nil && contextConfig.Cluster != "" {
			cluster := cfg.Clusters[contextConfig.Cluster]
			if cluster != nil {
				return contextConfig.Cluster, cluster, nil
			}
		}
	}

	if len(cfg.Clusters) == 1 {
		for name, cluster := range cfg.Clusters {
			return name, cluster, nil
		}
	}

	return "", nil, fmt.Errorf(
		"kubeconfig has no unambiguous current cluster: currentContext=%q clusters=%d",
		cfg.CurrentContext,
		len(cfg.Clusters),
	)
}

func currentAuthInfoEntry(
	cfg *clientcmdapi.Config,
) (string, *clientcmdapi.AuthInfo, error) {
	if cfg.CurrentContext != "" {
		contextConfig := cfg.Contexts[cfg.CurrentContext]
		if contextConfig != nil && contextConfig.AuthInfo != "" {
			authInfo := cfg.AuthInfos[contextConfig.AuthInfo]
			if authInfo != nil {
				return contextConfig.AuthInfo, authInfo, nil
			}
		}
	}

	if len(cfg.AuthInfos) == 1 {
		for name, authInfo := range cfg.AuthInfos {
			return name, authInfo, nil
		}
	}

	contextNames := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		contextNames = append(contextNames, name)
	}

	authInfoNames := make([]string, 0, len(cfg.AuthInfos))
	for name := range cfg.AuthInfos {
		authInfoNames = append(authInfoNames, name)
	}

	return "", nil, fmt.Errorf(
		"kubeconfig has no unambiguous current auth info: currentContext=%q contexts=%v authInfos=%v",
		cfg.CurrentContext,
		contextNames,
		authInfoNames,
	)
}

func (p *PlatformRecoverySubroutine) ensureOpenFGAStores(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	// The restored kcp-admin kubeconfig can still be denied by the restored
	// authorization policy at this point. Compare and repair Store resources
	// with the destination recovery credential instead.
	dynamicClient, err := p.kcpRecoveryDynamicClient(ctx, orgsLogicalClusterPath)
	if err != nil {
		return false, err
	}

	stores, err := dynamicClient.Resource(storeGVR).List(
		ctx,
		metav1.ListOptions{},
	)
	if err != nil {
		return false, fmt.Errorf("list restored Store resources: %w", err)
	}

	openFGAStores, err := p.listOpenFGAStores(ctx)
	if err != nil {
		return false, err
	}

	log.Info().
		Int("storeResources", len(stores.Items)).
		Int("openFGAStores", len(openFGAStores)).
		Msg("comparing restored Store resources with OpenFGA")

	stale := make([]*unstructured.Unstructured, 0)
	for index := range stores.Items {
		store := &stores.Items[index]
		storeID, _, _ := unstructured.NestedString(
			store.Object,
			"status",
			"storeId",
		)
		if storeID == "" {
			log.Info().
				Str("store", store.GetName()).
				Msg("Store has no status.storeId and is waiting for reconciliation")
			continue
		}

		if _, exists := openFGAStores[storeID]; !exists {
			stale = append(stale, store)

			log.Info().
				Str("store", store.GetName()).
				Str("storeID", storeID).
				Msg("detected stale OpenFGA Store status")

			continue
		}

		// A PostgreSQL restore can preserve the Store row while losing the
		// authorization model that the restored Store status references. The
		// Store ID alone is therefore not sufficient evidence that the Store is
		// usable. Clear this stale status as well, so security-operator can
		// recreate the model and its tuples.
		modelID, _, _ := unstructured.NestedString(
			store.Object,
			"status",
			"authorizationModelId",
		)
		if modelID == "" {
			continue
		}

		modelExists, err := p.openFGAAuthorizationModelExists(ctx, storeID, modelID)
		if err != nil {
			return false, fmt.Errorf(
				"check authorization model for Store %s: %w",
				store.GetName(),
				err,
			)
		}
		if !modelExists {
			stale = append(stale, store)

			log.Info().
				Str("store", store.GetName()).
				Str("storeID", storeID).
				Str("authorizationModelID", modelID).
				Msg("detected Store status referencing a missing OpenFGA authorization model")
		}
	}

	securityOperator := platformDeployment("security-operator")
	if len(stale) > 0 {
		changed, err := securityOperator.scale(ctx, p.client, 0)
		if err != nil {
			return false, fmt.Errorf(
				"scale down %s/%s: %w",
				securityOperator.Namespace,
				securityOperator.Name,
				err,
			)
		}
		if changed {
			log.Info().
				Str("namespace", securityOperator.Namespace).
				Str("workload", securityOperator.Name).
				Int("staleStores", len(stale)).
				Msg("scaled security operator down before clearing stale Store statuses")

			return false, nil
		}

		down, err := securityOperator.ready(ctx, p.client)
		if err != nil {
			return false, err
		}
		if !down {
			log.Info().
				Str("namespace", securityOperator.Namespace).
				Str("workload", securityOperator.Name).
				Msg("waiting for security operator to stop")

			return false, nil
		}

		for _, store := range stale {
			storeID, _, _ := unstructured.NestedString(
				store.Object,
				"status",
				"storeId",
			)

			store.Object["status"] = map[string]any{}
			if _, err := dynamicClient.
				Resource(storeGVR).
				UpdateStatus(ctx, store, metav1.UpdateOptions{}); err != nil {
				return false, fmt.Errorf(
					"clear stale OpenFGA status for Store %s: %w",
					store.GetName(),
					err,
				)
			}

			log.Info().
				Str("store", store.GetName()).
				Str("staleStoreID", storeID).
				Msg("cleared stale OpenFGA Store status")
		}

		replicas := int32(1)
		if cm, err := ensureRestoreStateConfigMap(
			ctx,
			p.client,
			pr,
		); err == nil {
			if remembered, ok := rememberedReplicas(
				cm,
				securityOperator,
			); ok {
				replicas = remembered
			}
		}

		if _, err := securityOperator.scale(ctx, p.client, replicas); err != nil {
			return false, fmt.Errorf(
				"restore replicas for %s/%s: %w",
				securityOperator.Namespace,
				securityOperator.Name,
				err,
			)
		}

		log.Info().
			Str("namespace", securityOperator.Namespace).
			Str("workload", securityOperator.Name).
			Int32("replicas", replicas).
			Msg("restored security operator replicas")

		return false, nil
	}

	if cm, err := ensureRestoreStateConfigMap(
		ctx,
		p.client,
		pr,
	); err == nil {
		if replicas, ok := rememberedReplicas(cm, securityOperator); ok {
			if _, err := securityOperator.scale(ctx, p.client, replicas); err != nil {
				return false, err
			}
		}
	}

	securityReady, err := securityOperator.ready(ctx, p.client)
	if err != nil {
		return false, err
	}
	if !securityReady {
		log.Info().
			Str("namespace", securityOperator.Namespace).
			Str("workload", securityOperator.Name).
			Msg("waiting for security operator")

		return false, nil
	}

	for index := range stores.Items {
		store := &stores.Items[index]
		if !unstructuredConditionTrue(store, "Ready") {
			log.Info().
				Str("store", store.GetName()).
				Msg("waiting for Store Ready condition")

			return false, nil
		}

		storeID, _, _ := unstructured.NestedString(
			store.Object,
			"status",
			"storeId",
		)
		if storeID == "" {
			log.Info().
				Str("store", store.GetName()).
				Msg("waiting for Store status.storeId")

			return false, nil
		}

		if _, exists := openFGAStores[storeID]; !exists {
			log.Info().
				Str("store", store.GetName()).
				Str("storeID", storeID).
				Msg("waiting for Store ID to exist in OpenFGA")

			return false, nil
		}
	}

	log.Info().
		Int("stores", len(stores.Items)).
		Msg("all Store resources are ready and present in OpenFGA")

	return true, nil
}

func (p *PlatformRecoverySubroutine) accountsReady(
	ctx context.Context,
) (bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	// Account readiness is checked while restored authorization policies may
	// still reject kcp-admin through the front-proxy. Use the destination
	// recovery credential until identity recovery is complete.
	dynamicClient, err := p.kcpRecoveryDynamicClient(ctx, orgsLogicalClusterPath)
	if err != nil {
		return false, err
	}

	accounts, err := dynamicClient.Resource(accountGVR).List(
		ctx,
		metav1.ListOptions{},
	)
	if err != nil {
		return false, fmt.Errorf("list restored Account resources: %w", err)
	}

	for index := range accounts.Items {
		account := &accounts.Items[index]
		if !unstructuredConditionTrue(account, "Ready") {
			log.Info().
				Str("account", account.GetName()).
				Msg("waiting for Account Ready condition")

			return false, nil
		}
	}

	log.Info().
		Int("accounts", len(accounts.Items)).
		Msg("all restored Account resources are ready")

	return true, nil
}

func unstructuredConditionTrue(obj *unstructured.Unstructured, conditionType string) bool {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if condition["type"] == conditionType && condition["status"] == "True" {
			return true
		}
	}

	return false
}

func (p *PlatformRecoverySubroutine) listOpenFGAStores(
	ctx context.Context,
) (map[string]string, error) {
	log := logger.LoadLoggerFromContext(ctx)

	baseURL := os.Getenv("OPENFGA_API_URL")
	if baseURL == "" {
		baseURL = openFGAServiceURLDefault
	}

	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/stores")
	if err != nil {
		return nil, fmt.Errorf("parse OpenFGA stores URL: %w", err)
	}

	log.Info().
		Str("url", u.String()).
		Msg("listing OpenFGA stores")

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		u.String(),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create OpenFGA stores request: %w", err)
	}

	response, err := p.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("list OpenFGA stores: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"list OpenFGA stores: status=%d",
			response.StatusCode,
		)
	}

	var result struct {
		Stores []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"stores"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode OpenFGA stores: %w", err)
	}

	stores := make(map[string]string, len(result.Stores))
	for _, store := range result.Stores {
		stores[store.ID] = store.Name
	}

	log.Info().
		Int("stores", len(stores)).
		Msg("listed OpenFGA stores")

	return stores, nil
}

func (p *PlatformRecoverySubroutine) openFGAAuthorizationModelExists(
	ctx context.Context,
	storeID string,
	modelID string,
) (bool, error) {
	baseURL := os.Getenv("OPENFGA_API_URL")
	if baseURL == "" {
		baseURL = openFGAServiceURLDefault
	}

	// ReadAuthorizationModel maps authorization_model_not_found to HTTP 400 in
	// the OpenFGA version we deploy. ReadAuthorizationModels has a stable 200
	// response, so use it to determine whether the status model is still live.
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/stores/" + url.PathEscape(storeID) + "/authorization-models")
	if err != nil {
		return false, fmt.Errorf("parse OpenFGA authorization-model URL: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, fmt.Errorf("create OpenFGA authorization-model request: %w", err)
	}

	response, err := p.httpClient.Do(request)
	if err != nil {
		return false, fmt.Errorf("list OpenFGA authorization models: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("list OpenFGA authorization models: status=%d", response.StatusCode)
	}

	var result struct {
		AuthorizationModels []struct {
			ID string `json:"id"`
		} `json:"authorization_models"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decode OpenFGA authorization models: %w", err)
	}

	for _, model := range result.AuthorizationModels {
		if model.ID == modelID {
			return true, nil
		}
	}

	return false, nil
}

func workloadsReady(
	ctx context.Context,
	cl ctrlruntimeclient.Client,
	workloads []workloadRef,
) (bool, error) {
	log := logger.LoadLoggerFromContext(ctx)

	for _, workload := range workloads {
		ready, err := workload.ready(ctx, cl)

		if err != nil {
			return false, err
		}

		if !ready {
			log.Info().
				Str("namespace", workload.Namespace).
				Str("kind", workload.Kind).
				Str("workload", workload.Name).
				Msg("workload is not ready")

			return false, nil
		}
	}

	return true, nil
}
