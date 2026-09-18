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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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

	// Create kcp workspaces recursively
	err = r.createKcpResources(ctx, cfg, r.kcpDirectory, inst)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create kcp workspaces")
		return subroutines.OK(), gcerrors.Wrap(err, "Failed to create kcp workspaces")
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
		// IdPRegistration webhooks use the same security-operator CA.
		caBundles[fmt.Sprintf("%s.ca-bundle", IdPRegistrationValidatingWebhookName)] = caBundles[ipdKey]
		caBundles[fmt.Sprintf("%s.ca-bundle", IdPRegistrationMutatingWebhookName)] = caBundles[ipdKey]
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
