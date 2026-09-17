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

package subroutines

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/internal/metrics"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpapiv1alpha "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
)

type KcpsetupSubroutine struct {
	client       ctrlruntimeclient.Client
	kcpHelper    KcpHelper
	helm         HelmGetter
	kcpDirectory string
	// Cache for CA bundles to avoid redundant secret lookups
	caBundleCache map[string]string
	cfg           *config.OperatorConfig
	kcpUrl        string
}

const (
	KcpsetupSubroutineName      = "KcpsetupSubroutine"
	KcpsetupSubroutineFinalizer = "platform-mesh.core.platform-mesh.io/finalizer"
	fieldManagerKcpSetup        = "platform-mesh-kcp-setup"
)

func NewKcpsetupSubroutine(client ctrlruntimeclient.Client, helper KcpHelper, cfg *config.OperatorConfig, kcpdir string, kcpUrl string) *KcpsetupSubroutine {
	return &KcpsetupSubroutine{
		client:        client,
		kcpDirectory:  kcpdir,
		kcpHelper:     helper,
		helm:          DefaultHelmGetter{},
		caBundleCache: make(map[string]string),
		cfg:           cfg,
		kcpUrl:        kcpUrl,
	}
}

func (r *KcpsetupSubroutine) GetName() string {
	return KcpsetupSubroutineName
}

func (r *KcpsetupSubroutine) Finalize(
	_ context.Context, _ ctrlruntimeclient.Object,
) (subroutines.Result, error) {
	return subroutines.OK(), nil
}

func (r *KcpsetupSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string { // coverage-ignore
	return []string{KcpsetupSubroutineFinalizer}
}

func (r *KcpsetupSubroutine) Process(ctx context.Context, runtimeObj ctrlruntimeclient.Object) (res subroutines.Result, err error) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if err != nil {
			labelResult = "error"
		}
		metrics.SubroutineTotal.WithLabelValues(r.GetName(), labelResult).Inc()
		metrics.SubroutineDuration.WithLabelValues(r.GetName()).Observe(time.Since(start).Seconds())
	}()
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)

	inst := runtimeObj.(*pmcorev1alpha1.PlatformMesh)
	log.Debug().Str("subroutine", r.GetName()).Str("name", inst.Name).Msg("Processing Platform Mesh resource")

	rootShard := &unstructured.Unstructured{}
	rootShard.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "RootShard"})
	// Wait for root shard to be ready
	err = r.client.Get(ctx, types.NamespacedName{Name: operatorCfg.KCP.RootShardName, Namespace: operatorCfg.KCP.Namespace}, rootShard)
	if err != nil || !matchesConditionWithStatus(rootShard, "Available", "True") {
		log.Info().Msg("RootShard is not ready..")
		return subroutines.StopWithRequeue(DefaultRequeueInterval, "RootShard is not ready"), nil
	}

	frontProxy := &unstructured.Unstructured{}
	frontProxy.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "FrontProxy"})
	// Wait for front proxy to be ready
	err = r.client.Get(ctx, types.NamespacedName{Name: operatorCfg.KCP.FrontProxyName, Namespace: operatorCfg.KCP.Namespace}, frontProxy)
	if err != nil || !matchesConditionWithStatus(frontProxy, "Available", "True") {
		log.Info().Msg("FrontProxy is not ready..")
		return subroutines.StopWithRequeue(DefaultRequeueInterval, "FrontProxy is not ready"), nil
	}

	// Build kcp kubeconfig
	cfg, err := buildKubeconfig(ctx, r.client, getExternalKcpHost(inst, r.cfg))
	if err != nil {
		log.Error().Err(err).Msg("Failed to build kubeconfig")
		return subroutines.OK(), gcerrors.Wrap(err, "Failed to build kubeconfig")
	}

	// Must run before the legacy-binding migrations below: they rebind onto exports this step creates.
	err = r.createKcpResources(ctx, cfg, r.kcpDirectory, inst)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create kcp workspaces")
		return subroutines.OK(), gcerrors.Wrap(err, "Failed to create kcp workspaces")
	}

	// Migrate root:orgs off the pre-split core.platform-mesh.io binding.
	if err = r.migrateLegacyOrgsBinding(ctx, cfg); err != nil {
		log.Error().Err(err).Msg("Failed to migrate legacy root:orgs core.platform-mesh.io binding")
		return subroutines.OK(), gcerrors.Wrap(err, "Failed to migrate legacy root:orgs core.platform-mesh.io binding")
	}

	// Same migration for existing provider workspaces still on the old binding.
	if err = r.migrateLegacyProviderBindings(ctx, cfg, inst); err != nil {
		log.Error().Err(err).Msg("Failed to migrate legacy provider workspace bindings")
		return subroutines.OK(), gcerrors.Wrap(err, "Failed to migrate legacy provider workspace bindings")
	}

	// apply extra workspaces
	err = r.applyExtraWorkspaces(ctx, cfg, inst)
	if err != nil {
		log.Error().Err(err).Msg("Failed to apply extra workspaces")
		return subroutines.OK(), gcerrors.Wrap(err, "Failed to apply extra workspaces")
	}

	// update workspace status
	inst.Status.KcpWorkspaces = []pmcorev1alpha1.KcpWorkspace{
		{
			Name:  "root:platform-mesh-system",
			Phase: "Ready",
		},
		{
			Name:  "root:orgs",
			Phase: "Ready",
		},
	}

	log.Debug().Msg("Successful kcp setup")

	return subroutines.OK(), nil
}

func (r *KcpsetupSubroutine) createKcpResources(ctx context.Context, config *rest.Config, dir string, inst *pmcorev1alpha1.PlatformMesh) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	// Get API export hashes
	apiExportHashes, err := r.getAPIExportHashInventory(ctx, config)
	if err != nil {
		log.Err(err).Msg("Failed to get APIExport hash inventory")
		return gcerrors.Wrap(err, "Failed to get APIExport hash inventory")
	}

	// Get CA bundle data
	caBundles, err := r.getCABundleInventory(ctx, inst)
	if err != nil {
		log.Err(err).Msg("Failed to get CA bundle inventory")
		return gcerrors.Wrap(err, "Failed to get CA bundle inventory")
	}

	// Build templateData as map[string]any to support both strings and arrays
	templateData := make(map[string]any)
	for k, v := range caBundles {
		templateData[k] = v
	}
	for k, v := range apiExportHashes {
		templateData[k] = v
	}

	for k, v := range getExposureParams(inst).templateVars(r.cfg.KCP) {
		templateData[k] = v
	}
	templateData["featureDisableEmailVerification"] = HasFeatureToggle(inst, "feature-disable-email-verification")
	templateData["featureDisableContentConfigurations"] = HasFeatureToggle(inst, "feature-disable-contentconfigurations")
	templateData["featureEnableTerminalControllerManager"] = HasFeatureToggle(inst, "feature-enable-terminal-controller-manager")
	templateData["featureDisableIDPWebhook"] = HasFeatureToggle(inst, FeatureDisableIDPWebhook)
	templateData["registrationAllowed"] = r.cfg.IDP.RegistrationAllowed
	templateData["welcomeAdditionalRedirectUris"] = r.cfg.IDP.WelcomeAdditionalRedirectUris
	templateData["welcomeAdditionalPostLogoutRedirectUris"] = r.cfg.IDP.WelcomeAdditionalPostLogoutRedirectUris
	templateData["userClaim"] = r.cfg.IDP.UserClaim

	pmSystemClient, err := r.kcpHelper.NewKcpClient(config, "root:platform-mesh-system")
	if err != nil {
		log.Err(err).Msg("Failed to create kcp client for platform-mesh-system workspace")
		return gcerrors.Wrap(err, "Failed to create kcp client for platform-mesh-system workspace")
	}

	templateData["welcomeAudiences"] = []string{}

	var ipc unstructured.Unstructured
	ipc.SetGroupVersionKind(schema.GroupVersionKind{Group: "core.platform-mesh.io", Version: "v1alpha1", Kind: "IdentityProviderConfiguration"})

	err = pmSystemClient.Get(ctx, types.NamespacedName{Name: "welcome"}, &ipc)
	if err == nil {
		managedClients, found, err := unstructured.NestedMap(ipc.Object, "status", "managedClients")
		if err != nil {
			log.Err(err).Msg("Failed to get managedClients from IdentityProviderConfiguration 'welcome'")
			return gcerrors.Wrap(err, "Failed to get managedClients from IdentityProviderConfiguration 'welcome'")
		}

		if found && len(managedClients) > 0 {
			var clientIds []string
			for clientName, clientData := range managedClients {
				clientMap, ok := clientData.(map[string]any)
				if !ok {
					log.Warn().Str("client", clientName).Msg("Invalid client data structure, skipping")
					continue
				}
				clientId, ok := clientMap["clientId"].(string)
				if !ok || clientId == "" {
					log.Debug().Str("client", clientName).Msg("No clientId found for client, skipping")
					continue
				}
				clientIds = append(clientIds, clientId)
			}

			if len(clientIds) > 0 {
				templateData["welcomeAudiences"] = clientIds
			}
		}
	}

	err = ApplyDirStructure(ctx, dir, "root", config, templateData, inst, r.kcpHelper)
	if err != nil {
		log.Err(err).Msg("Failed to apply dir structure")
		return gcerrors.Wrap(err, "Failed to apply dir structure")
	}

	return nil
}

func (r *KcpsetupSubroutine) getCABundleInventory(
	ctx context.Context,
	inst *pmcorev1alpha1.PlatformMesh,
) (map[string]string, error) {
	log := logger.LoadLoggerFromContext(ctx)

	// If we already have cached results, return them
	if len(r.caBundleCache) > 0 {
		return r.caBundleCache, nil
	}

	caBundles := make(map[string]string)

	// Get default webhook CA bundle
	webhookConfig := DEFAULT_WEBHOOK_CONFIGURATION
	caData, err := r.getCaBundle(ctx, &webhookConfig)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get CA bundle")
		return nil, gcerrors.Wrap(err, "Failed to get CA bundle")
	}

	key := fmt.Sprintf("%s.ca-bundle", webhookConfig.WebhookRef.Name)
	b64Data := base64.StdEncoding.EncodeToString(caData)
	caBundles[key] = b64Data

	// Get Identity Provider validating webhook CA bundle (security-operator webhook)
	// Skip if webhook is disabled via feature toggle
	if HasFeatureToggle(inst, FeatureDisableIDPWebhook) != "true" {
		ipdValidatingWebhookConfig := DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION
		ipdCaData, err := r.getCaBundle(ctx, &ipdValidatingWebhookConfig)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get Identity Provider ValidatingWebhook CA bundle")
			return nil, gcerrors.Wrap(err, "Failed to get Identity Provider ValidatingWebhook CA bundle")
		}
		ipdKey := fmt.Sprintf("%s.ca-bundle", ipdValidatingWebhookConfig.WebhookRef.Name)
		caBundles[ipdKey] = base64.StdEncoding.EncodeToString(ipdCaData)
	}

	// Get validating webhook CA bundle
	validatingWebhookConfig := DEFAULT_VALIDATING_WEBHOOK_CONFIGURATION
	validatingCaData, err := r.getCaBundle(ctx, &validatingWebhookConfig)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get ValidatingWebhook CA bundle")
		return nil, gcerrors.Wrap(err, "Failed to get ValidatingWebhook CA bundle")
	}

	validatingKey := fmt.Sprintf("%s.ca-bundle", validatingWebhookConfig.WebhookRef.Name)
	validatingB64Data := base64.StdEncoding.EncodeToString(validatingCaData)
	caBundles[validatingKey] = validatingB64Data

	domainCA, err := r.getCaBundle(ctx, &pmcorev1alpha1.WebhookConfiguration{
		SecretData: r.cfg.Subroutines.KcpSetup.DomainCertificateCASecretKey,
		SecretRef: pmcorev1alpha1.SecretReference{
			Name:      r.cfg.Subroutines.KcpSetup.DomainCertificateCASecretName,
			Namespace: "platform-mesh-system",
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get Domain CA bundle")
		return nil, gcerrors.Wrap(err, "Failed to get Domain CA bundle")
	}

	caBundles["domainCA"] = base64.StdEncoding.EncodeToString(domainCA)
	caBundles["domainCADec"] = string(domainCA)

	// Cache the results
	r.caBundleCache = caBundles

	return caBundles, nil
}

func (r *KcpsetupSubroutine) getCaBundle(
	ctx context.Context,
	webhookConfig *pmcorev1alpha1.WebhookConfiguration,
) ([]byte, error) {
	log := logger.LoadLoggerFromContext(ctx)

	caSecret := corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{
		Name:      webhookConfig.SecretRef.Name,
		Namespace: webhookConfig.SecretRef.Namespace,
	}, &caSecret)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get ca secret")
		return nil, gcerrors.Wrap(err, "Failed to get ca secret: %s/%s", webhookConfig.SecretRef.Namespace, webhookConfig.SecretRef.Name)
	}

	caData, ok := caSecret.Data[webhookConfig.SecretData]
	if !ok {
		log.Error().Msg("Failed to get caData from secret")
		return nil, gcerrors.New("failed to get caData from secret: %s/%s, key: %s", webhookConfig.SecretRef.Namespace, webhookConfig.SecretRef.Name, webhookConfig.SecretData)
	}

	decodedCaData := caData
	return decodedCaData, nil
}

func (r *KcpsetupSubroutine) getAPIExportHashInventory(ctx context.Context, config *rest.Config) (map[string]string, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	inventory := map[string]string{}

	cs, err := r.kcpHelper.NewKcpClient(config, "root")
	if err != nil {
		return inventory, err
	}

	apiExport := kcpapiv1alpha.APIExport{}
	err = cs.Get(ctx, types.NamespacedName{Name: "tenancy.kcp.io"}, &apiExport)
	if err != nil {
		log.Err(err).Msg("Failed to get APIExport for tenancy.kcp.io")
		return inventory, gcerrors.Wrap(err, "Failed to get APIExport for tenancy.kcp.io")
	}
	inventory["apiExportRootTenancyKcpIoIdentityHash"] = apiExport.Status.IdentityHash

	err = cs.Get(ctx, types.NamespacedName{Name: "shards.core.kcp.io"}, &apiExport)
	if err != nil {
		log.Err(err).Msg("Failed to get APIExport for shards.core.kcp.io")
		return inventory, gcerrors.Wrap(err, "Failed to get APIExport for shards.core.kcp.io")
	}
	inventory["apiExportRootShardsKcpIoIdentityHash"] = apiExport.Status.IdentityHash

	err = cs.Get(ctx, types.NamespacedName{Name: "topology.kcp.io"}, &apiExport)
	if err != nil {
		log.Err(err).Msg("Failed to get APIExport for topology.kcp.io")
		return inventory, gcerrors.Wrap(err, "Failed to get APIExport for topology.kcp.io")
	}
	inventory["apiExportRootTopologyKcpIoIdentityHash"] = apiExport.Status.IdentityHash

	return inventory, nil
}

const corePlatformMeshIOExport = "core.platform-mesh.io"

// findLegacyBinding returns the APIBinding, if any, that still references the pre-split
// core.platform-mesh.io export and has resourceMarker among its locked boundResources.
func findLegacyBinding(bindings *unstructured.UnstructuredList, resourceMarker string) *unstructured.Unstructured {
	for i := range bindings.Items {
		b := &bindings.Items[i]
		exportName, _, _ := unstructured.NestedString(b.Object, "spec", "reference", "export", "name")
		if exportName != corePlatformMeshIOExport {
			continue
		}
		boundResources, _, _ := unstructured.NestedSlice(b.Object, "status", "boundResources")
		for _, br := range boundResources {
			if resource, ok := br.(map[string]any); ok && resource["resource"] == resourceMarker {
				return b
			}
		}
	}
	return nil
}

// deleteWithSuccessorWait sets deletionPolicy=WaitForSuccessor and deletes binding; kcp (>=v0.33.0)
// holds the finalizer until a same-identity successor adopts its instances. Apply a successor first, then call waitForBindingGone.
func deleteWithSuccessorWait(ctx context.Context, client ctrlruntimeclient.Client, binding *unstructured.Unstructured) error {
	policyPatch := ctrlruntimeclient.RawPatch(types.MergePatchType, []byte(`{"spec":{"deletionPolicy":"WaitForSuccessor"}}`))
	if err := client.Patch(ctx, binding, policyPatch); err != nil {
		return gcerrors.Wrap(err, "Failed to set deletionPolicy=WaitForSuccessor on binding %s", binding.GetName())
	}
	if err := client.Delete(ctx, binding); err != nil && !apierrors.IsNotFound(err) {
		return gcerrors.Wrap(err, "Failed to delete binding %s", binding.GetName())
	}
	return nil
}

// waitForBindingGone waits for a deleted binding to actually disappear, i.e. for kcp to find and
// apply a successor for every one of its bound resources.
func waitForBindingGone(ctx context.Context, client ctrlruntimeclient.Client, bindingName string) error {
	err := wait.PollUntilContextTimeout(ctx, time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		check := &unstructured.Unstructured{}
		check.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"})
		getErr := client.Get(ctx, types.NamespacedName{Name: bindingName}, check)
		return apierrors.IsNotFound(getErr), nil
	})
	if err != nil {
		return gcerrors.Wrap(err, "Timed out waiting for binding %s to be adopted and removed", bindingName)
	}
	return nil
}

// applyBinding creates or updates an APIBinding named name, referencing export at path.
func applyBinding(ctx context.Context, client ctrlruntimeclient.Client, name, export, path string) error {
	fresh := &unstructured.Unstructured{}
	fresh.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"})
	fresh.SetName(name)
	if err := unstructured.SetNestedMap(fresh.Object, map[string]any{
		"export": map[string]any{"name": export, "path": path},
	}, "spec", "reference"); err != nil {
		return gcerrors.Wrap(err, "Failed to build %s binding", name)
	}
	return client.Apply(ctx, ctrlruntimeclient.ApplyConfigurationFromUnstructured(fresh),
		ctrlruntimeclient.FieldOwner(fieldManagerKcpSetup), ctrlruntimeclient.ForceOwnership)
}

// migrateLegacyOrgsBinding swaps root:orgs off the pre-split core.platform-mesh.io binding.
// orgs.core.platform-mesh.io shares its identity, so kcp adopts the Store/AuthorizationModel instances automatically.
func (r *KcpsetupSubroutine) migrateLegacyOrgsBinding(ctx context.Context, config *rest.Config) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	orgsClient, err := r.kcpHelper.NewKcpClient(config, "root:orgs")
	if err != nil {
		return gcerrors.Wrap(err, "Failed to create kcp client for root:orgs workspace")
	}

	bindings := &unstructured.UnstructuredList{}
	bindings.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBindingList"})
	if err := orgsClient.List(ctx, bindings); err != nil {
		// root:orgs may not exist yet on a fresh install; nothing to migrate.
		return nil //nolint:nilerr
	}

	legacyBinding := findLegacyBinding(bindings, "stores")
	if legacyBinding == nil {
		return nil
	}

	log.Info().Str("binding", legacyBinding.GetName()).
		Msg("root:orgs still on pre-split core.platform-mesh.io binding, migrating to orgs.core.platform-mesh.io")

	// createKcpResources already applied the fresh successor bindings, so they're in place by now.
	if err := deleteWithSuccessorWait(ctx, orgsClient, legacyBinding); err != nil {
		return gcerrors.Wrap(err, "Failed to migrate legacy core.platform-mesh.io binding in root:orgs")
	}
	if err := waitForBindingGone(ctx, orgsClient, legacyBinding.GetName()); err != nil {
		return gcerrors.Wrap(err, "Failed to migrate legacy core.platform-mesh.io binding in root:orgs")
	}

	log.Info().Msg("legacy core.platform-mesh.io binding removed from root:orgs, resources adopted by orgs.core.platform-mesh.io")
	return nil
}

// providerWorkspaceTypePath is the provider WorkspaceType's own path, as checked against PlatformMesh.spec.kcp.extraDefaultAPIBindings.
const providerWorkspaceTypePath = "root:provider"

// uiExportPath returns the extraDefaultAPIBindings path for ui.platform-mesh.io on provider workspaces, and whether the deployer opted in at all.
func uiExportPath(inst *pmcorev1alpha1.PlatformMesh) (string, bool) {
	for _, b := range inst.Spec.Kcp.ExtraDefaultAPIBindings {
		if b.WorkspaceTypePath == providerWorkspaceTypePath && b.Export == "ui.platform-mesh.io" {
			return b.Path, true
		}
	}
	return "", false
}

// migrateLegacyProviderBindings does for existing provider workspaces what migrateLegacyOrgsBinding does for root:orgs.
func (r *KcpsetupSubroutine) migrateLegacyProviderBindings(ctx context.Context, config *rest.Config, inst *pmcorev1alpha1.PlatformMesh) error {
	providersClient, err := r.kcpHelper.NewKcpClient(config, "root:providers")
	if err != nil {
		return gcerrors.Wrap(err, "Failed to create kcp client for root:providers workspace")
	}

	var workspaces kcptenancyv1alpha.WorkspaceList
	if err := providersClient.List(ctx, &workspaces); err != nil {
		// root:providers may not exist yet on a fresh install; nothing to migrate.
		return nil //nolint:nilerr
	}

	uiPath, uiOptedIn := uiExportPath(inst)

	for _, ws := range workspaces.Items {
		if ws.Name == "system" {
			continue
		}
		if err := r.migrateLegacyProviderBinding(ctx, config, ws.Name, uiPath, uiOptedIn); err != nil {
			return gcerrors.Wrap(err, "Failed to migrate legacy provider binding for %s", ws.Name)
		}
	}
	return nil
}

// migrateLegacyProviderBinding swaps a provider workspace off the old, pre-split core.platform-mesh.io
// binding. If opted into ui.platform-mesh.io, it shares identity and kcp adopts the instances automatically; otherwise they're deleted as before.
func (r *KcpsetupSubroutine) migrateLegacyProviderBinding(
	ctx context.Context, config *rest.Config, providerName, uiPath string, uiOptedIn bool,
) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	providerClient, err := r.kcpHelper.NewKcpClient(config, "root:providers:"+providerName)
	if err != nil {
		return gcerrors.Wrap(err, "Failed to create kcp client for provider workspace %s", providerName)
	}

	bindings := &unstructured.UnstructuredList{}
	bindings.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBindingList"})
	if err := providerClient.List(ctx, bindings); err != nil {
		return nil //nolint:nilerr
	}

	legacyBinding := findLegacyBinding(bindings, "contentconfigurations")
	if legacyBinding == nil {
		return nil
	}

	log.Info().Str("provider", providerName).Str("binding", legacyBinding.GetName()).
		Msg("provider workspace still on pre-split core.platform-mesh.io binding, migrating")

	if !uiOptedIn {
		// No successor will ever exist here, so WaitForSuccessor would hold forever; delete outright.
		for _, kind := range []string{"ContentConfiguration", "ProviderMetadata"} {
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(schema.GroupVersionKind{Group: "ui.platform-mesh.io", Version: "v1alpha1", Kind: kind + "List"})
			if err := providerClient.List(ctx, list); err != nil {
				return gcerrors.Wrap(err, "Failed to list %ss in provider workspace %s", kind, providerName)
			}
			for i := range list.Items {
				obj := &list.Items[i]
				if len(obj.GetFinalizers()) == 0 {
					continue
				}
				patch := ctrlruntimeclient.RawPatch(types.JSONPatchType, []byte(`[{"op":"remove","path":"/metadata/finalizers"}]`))
				if err := providerClient.Patch(ctx, obj, patch); err != nil {
					return gcerrors.Wrap(err, "Failed to clear finalizers on %s in provider workspace %s", kind, providerName)
				}
			}
			if len(list.Items) > 0 {
				log.Warn().Str("provider", providerName).Str("kind", kind).Int("count", len(list.Items)).
					Msg("provider workspace not opted into ui.platform-mesh.io, existing data is now unreachable and will be deleted")
			}
		}
		if err := providerClient.Delete(ctx, legacyBinding); err != nil && !apierrors.IsNotFound(err) {
			return gcerrors.Wrap(err, "Failed to delete legacy core.platform-mesh.io binding in provider workspace %s", providerName)
		}
		if err := applyBinding(ctx, providerClient, "core.platform-mesh.io", corePlatformMeshIOExport, "root:platform-mesh-system"); err != nil {
			return gcerrors.Wrap(err, "Failed to apply core.platform-mesh.io binding for provider workspace %s", providerName)
		}
		log.Info().Str("provider", providerName).Msg("legacy core.platform-mesh.io binding removed, fresh core.platform-mesh.io binding applied")
		return nil
	}

	// Apply the fresh bindings before deleting the legacy one, or the CRD has a zero-binding
	// window where it can get garbage collected regardless of kcp's adoption bookkeeping.
	if err := applyBinding(ctx, providerClient, "core.platform-mesh.io", corePlatformMeshIOExport, "root:platform-mesh-system"); err != nil {
		return gcerrors.Wrap(err, "Failed to apply core.platform-mesh.io binding for provider workspace %s", providerName)
	}
	if err := applyBinding(ctx, providerClient, "ui.platform-mesh.io", "ui.platform-mesh.io", uiPath); err != nil {
		return gcerrors.Wrap(err, "Failed to apply ui.platform-mesh.io binding for provider workspace %s", providerName)
	}
	if err := deleteWithSuccessorWait(ctx, providerClient, legacyBinding); err != nil {
		return gcerrors.Wrap(err, "Failed to migrate legacy core.platform-mesh.io binding in provider workspace %s", providerName)
	}
	if err := waitForBindingGone(ctx, providerClient, legacyBinding.GetName()); err != nil {
		return gcerrors.Wrap(err, "Failed to migrate legacy core.platform-mesh.io binding in provider workspace %s", providerName)
	}

	log.Info().Str("provider", providerName).
		Msg("legacy core.platform-mesh.io binding removed, fresh bindings applied, content adopted by ui.platform-mesh.io")
	return nil
}

func (r *KcpsetupSubroutine) applyExtraWorkspaces(ctx context.Context, config *rest.Config, inst *pmcorev1alpha1.PlatformMesh) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	if inst.Spec.Kcp.ExtraWorkspaces == nil {
		return nil
	}

	for _, wsDecl := range inst.Spec.Kcp.ExtraWorkspaces {
		lastColon := strings.LastIndex(wsDecl.Path, ":")
		if lastColon == -1 {
			log.Warn().Str("path", wsDecl.Path).Msg("Invalid workspace path format for extraWorkspace, skipping. Must be 'parent:name'.")
			continue
		}
		parentPath := wsDecl.Path[:lastColon]
		workspaceName := wsDecl.Path[lastColon+1:]

		log.Debug().Str("parentPath", parentPath).Str("workspaceName", workspaceName).Msg("Processing extra workspace")

		k8sClient, err := r.kcpHelper.NewKcpClient(config, parentPath)
		if err != nil {
			return gcerrors.Wrap(err, "Failed to create kcp client for parent workspace %s", parentPath)
		}

		ws := &kcptenancyv1alpha.Workspace{}
		ws.APIVersion = kcptenancyv1alpha.SchemeGroupVersion.String()
		ws.Kind = "Workspace"
		ws.Name = workspaceName
		ws.Spec.Type = &kcptenancyv1alpha.WorkspaceTypeReference{
			Name: kcptenancyv1alpha.WorkspaceTypeName(wsDecl.Type.Name),
			Path: wsDecl.Type.Path,
		}

		unstructuredWs, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ws)
		if err != nil {
			return gcerrors.Wrap(err, "failed to convert workspace to unstructured")
		}
		obj := unstructured.Unstructured{Object: unstructuredWs}

		err = k8sClient.Patch(ctx, &obj, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(fieldManagerKcpSetup)) //nolint:staticcheck // Apply via Patch is required for unstructured objects
		if err != nil {
			return gcerrors.Wrap(err, "Failed to apply extra workspace: %s", obj.GetName())
		}
		log.Info().Str("workspace", wsDecl.Path).Msg("Applied extra workspace")
	}
	return nil
}

func getExtraDefaultApiBindings(obj unstructured.Unstructured, workspacePath string, inst *pmcorev1alpha1.PlatformMesh) []pmcorev1alpha1.DefaultAPIBindingConfiguration {
	if inst.Spec.Kcp.ExtraDefaultAPIBindings == nil {
		return nil
	}
	res := []pmcorev1alpha1.DefaultAPIBindingConfiguration{}
	for _, binding := range inst.Spec.Kcp.ExtraDefaultAPIBindings {
		workspaceTypePath := fmt.Sprintf("%s:%s", workspacePath, obj.GetName())
		if binding.WorkspaceTypePath == workspaceTypePath {
			found := binding
			res = append(res, found)
		}
	}

	return res
}

func HasFeatureToggle(inst *pmcorev1alpha1.PlatformMesh, name string) string {
	for _, ft := range inst.Spec.FeatureToggles {
		if ft.Name == name {
			return "true"
		}
	}
	return "false"
}
