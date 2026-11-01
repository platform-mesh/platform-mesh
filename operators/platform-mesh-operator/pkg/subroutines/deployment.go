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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/rs/zerolog/log"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/internal/metrics"
	"go.platform-mesh.io/platform-mesh-operator/pkg/merge"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const DeploymentSubroutineName = "DeploymentSubroutine"

const (
	deploymentTechFluxCD = "fluxcd"
	deploymentTechArgoCD = "argocd"
)

type DeploymentSubroutine struct {
	clientInfra              ctrlruntimeclient.Client
	clientRuntime            ctrlruntimeclient.Client
	cfg                      *pmconfig.CommonServiceConfig
	workspaceDirectory       string
	gotemplatesInfraDir      string
	gotemplatesComponentsDir string
	cfgOperator              *config.OperatorConfig
	imageVersionStore        *ImageVersionStore
}

const (
	profileConfigMapKey           = "profile.yaml"
	defaultProfileConfigMapSuffix = "-profile"
	// argoPlaceholderRepoURL is set by templates and replaced by ResourceSubroutine with actual OCM values
	argoPlaceholderRepoURL = "PLACEHOLDER_TO_BE_SET_BY_RESOURCE_SUBROUTINE"
)

func NewDeploymentSubroutine(clientRuntime ctrlruntimeclient.Client, clientInfra ctrlruntimeclient.Client, cfg *pmconfig.CommonServiceConfig, operatorCfg *config.OperatorConfig) *DeploymentSubroutine {
	workspaceDir := filepath.Join(operatorCfg.WorkspaceDir, "/manifests/k8s/")
	// gotemplates is at the root level, relative to WorkspaceDir
	gotemplatesInfraDir := filepath.Join(operatorCfg.WorkspaceDir, "gotemplates/infra")
	gotemplatesComponentsDir := filepath.Join(operatorCfg.WorkspaceDir, "gotemplates/components")

	sub := &DeploymentSubroutine{
		cfg:                      cfg,
		clientInfra:              clientInfra,
		clientRuntime:            clientRuntime,
		workspaceDirectory:       workspaceDir,
		gotemplatesInfraDir:      gotemplatesInfraDir,
		gotemplatesComponentsDir: gotemplatesComponentsDir,
		cfgOperator:              operatorCfg,
	}

	return sub
}

// SetImageVersionStore sets the shared ImageVersionStore used to merge
// Resource-managed image versions into ArgoCD Application helm values.
func (r *DeploymentSubroutine) SetImageVersionStore(store *ImageVersionStore) {
	r.imageVersionStore = store
}

// getProfileConfigMap returns the profile ConfigMap for the given instance.
func (r *DeploymentSubroutine) getProfileConfigMap(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh) (*corev1.ConfigMap, error) {
	var configMapName, configMapNamespace string
	if inst.Spec.ProfileConfigMap != nil {
		configMapName = inst.Spec.ProfileConfigMap.Name
		configMapNamespace = inst.Spec.ProfileConfigMap.Namespace
		if configMapNamespace == "" {
			configMapNamespace = inst.Namespace
		}
	} else {
		// Use default ConfigMap name
		configMapName = inst.Name + defaultProfileConfigMapSuffix
		configMapNamespace = inst.Namespace
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: configMapNamespace,
		},
	}

	// Try to get existing ConfigMap
	err := r.clientRuntime.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(configMap), configMap)
	if err == nil {
		// ConfigMap exists, verify it has the required key
		if _, ok := configMap.Data[profileConfigMapKey]; !ok {
			return nil, fmt.Errorf("configMap %s/%s exists but does not contain key %s", configMapNamespace, configMapName, profileConfigMapKey)
		}
		return configMap, nil
	}

	return nil, err
}

// loadProfileSections returns infra and components profile sections as separate YAML strings
func (r *DeploymentSubroutine) loadProfileSections(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh) (infraProfile string, componentsProfile string, err error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	configMap, err := r.getProfileConfigMap(ctx, inst)
	if err != nil {
		return "", "", errors.Wrap(err, "failed to get or create profile ConfigMap")
	}

	profileYAML, ok := configMap.Data[profileConfigMapKey]
	if !ok {
		return "", "", fmt.Errorf("configMap %s/%s does not contain key %s", configMap.Namespace, configMap.Name, profileConfigMapKey)
	}

	// Parse unified profile
	var unifiedProfile map[string]any
	if err := yaml.Unmarshal([]byte(profileYAML), &unifiedProfile); err != nil {
		return "", "", errors.Wrap(err, "failed to parse profile YAML from ConfigMap")
	}

	// Extract infra section
	infraData := unifiedProfile["infra"]
	infraYAML, err := yaml.Marshal(infraData)
	if err != nil {
		return "", "", errors.Wrap(err, "failed to marshal infra profile")
	}

	// Extract components section
	componentsData := unifiedProfile["components"]
	componentsYAML, err := yaml.Marshal(componentsData)
	if err != nil {
		return "", "", errors.Wrap(err, "Failed to marshal components profile")
	}

	log.Debug().Str("configmap", configMap.Name).Str("namespace", configMap.Namespace).Msg("Loaded profile from ConfigMap")
	return string(infraYAML), string(componentsYAML), nil
}

func (r *DeploymentSubroutine) GetName() string {
	return DeploymentSubroutineName
}

func (r *DeploymentSubroutine) Finalize(_ context.Context, _ ctrlruntimeclient.Object) (subroutines.Result, error) {
	return subroutines.OK(), nil
}

func (r *DeploymentSubroutine) Finalizers(instance ctrlruntimeclient.Object) []string { // coverage-ignore
	return []string{}
}

func (r *DeploymentSubroutine) Process(ctx context.Context, runtimeObj ctrlruntimeclient.Object) (res subroutines.Result, err error) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if err != nil {
			labelResult = "error"
		}
		metrics.SubroutineTotal.WithLabelValues(r.GetName(), labelResult).Inc()
		metrics.SubroutineDuration.WithLabelValues(r.GetName()).Observe(time.Since(start).Seconds())
	}()
	inst := runtimeObj.(*pmcorev1alpha1.PlatformMesh)
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)

	// Create DeploymentComponents Version
	templateVars, err := TemplateVars(ctx, inst, r.clientRuntime)
	if err != nil {
		return subroutines.OK(), err
	}

	// Render and apply infra templates directly from gotemplates/infra/infra using profile
	oErr := r.renderAndApplyInfraTemplates(ctx, inst, templateVars)
	if oErr != nil {
		log.Error().Err(oErr).Msg("Failed to render and apply infra templates")
		return subroutines.OK(), oErr
	}
	log.Debug().Msg("Successfully rendered and applied infra templates")

	oErr = r.renderAndApplyRuntimeTemplates(ctx, inst, templateVars)
	if oErr != nil {
		log.Error().Err(oErr).Msg("Failed to render and apply runtime templates")
		return subroutines.OK(), oErr
	}
	log.Debug().Msg("Successfully rendered and applied runtime templates")

	// Render and apply components runtime templates (OCM Resources) early so that
	// ResourceSubroutine can create OCIRepositories on the infra cluster. Those
	// OCIRepositories are required by the infra HelmReleases (cert-manager, etcd-druid,
	// etc.) which are applied by renderAndApplyInfraTemplates above. Without the
	// OCIRepositories the cert-manager HelmRelease will never become Ready.
	oErr = r.renderAndApplyComponentsRuntimeTemplates(ctx, inst, templateVars)
	if oErr != nil {
		log.Error().Err(oErr).Msg("Failed to render and apply components runtime templates")
		return subroutines.OK(), oErr
	}
	log.Debug().Msg("Successfully rendered and applied components runtime templates")

	// Get deploymentTechnology from template vars or config (needed for checking resource readiness)
	tmplVars, err := r.templateVarsFromProfileInfra(ctx, inst, templateVars, r.cfgOperator)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get template vars for deploymentTechnology check")
		return subroutines.OK(), err
	}
	deploymentTech, _ := tmplVars["deploymentTechnology"].(string)
	if deploymentTech == "" {
		deploymentTech = deploymentTechFluxCD // default to fluxcd if not in profile
	}
	deploymentTech = strings.ToLower(deploymentTech)

	// Render and apply components infra templates (HelmReleases for services)
	disabled, oErr := r.renderAndApplyComponentsInfraTemplates(ctx, inst, templateVars)
	if oErr != nil {
		log.Error().Err(oErr).Msg("Failed to render and apply components infra templates")
		return subroutines.OK(), oErr
	}
	log.Debug().Msg("Successfully rendered and applied components infra templates")

	// delete only disabled HelmReleases
	oErr = deleteHelmReleases(ctx, r.clientInfra, disabled, log)
	if oErr != nil {
		log.Error().Err(oErr).Msg("Failed to delete HelmReleases")
		return subroutines.OK(), oErr
	}

	for _, crd := range []string{"issuers.cert-manager.io", "certificates.cert-manager.io"} {
		established, err := isCRDEstablished(ctx, r.clientRuntime, crd)
		if err != nil {
			log.Error().Err(err).Str("crd", crd).Msg("Failed to check cert-manager CRD")
			return subroutines.OK(), err
		}
		if !established {
			return subroutines.StopWithRequeue(DefaultRequeueInterval, fmt.Sprintf("cert-manager CRD %s is not established", crd)), nil
		}
	}

	webhookResult, oErr := r.manageAuthorizationWebhookSecrets(ctx, inst)
	if oErr != nil {
		log.Info().Msg("Failed to manage authorization webhook secrets")
		return subroutines.OK(), oErr
	}

	// Check if istio-proxy is injected
	if r.cfgOperator.Subroutines.Deployment.EnableIstio {
		// Wait for istiod release to be ready before continuing
		rel, err := getDeploymentResource(ctx, r.clientInfra, "istio-istiod", inst.Namespace, deploymentTech)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get istio-istiod resource")
			return subroutines.OK(), err
		}
		if deploymentTech == deploymentTechArgoCD {
			// For ArgoCD Applications, check status.sync.status and status.health.status directly
			syncStatus, found, _ := unstructured.NestedString(rel.Object, "status", "sync", "status")
			healthStatus, healthFound, _ := unstructured.NestedString(rel.Object, "status", "health", "status")

			if !found || syncStatus != "Synced" {
				return subroutines.StopWithRequeue(DefaultRequeueInterval, "istio-istiod Application is not synced"), nil
			}
			if !healthFound || healthStatus != "Healthy" {
				return subroutines.StopWithRequeue(DefaultRequeueInterval, "istio-istiod Application is not healthy"), nil
			}
		}

		if deploymentTech == deploymentTechFluxCD {
			// For FluxCD HelmReleases, check Ready condition
			if !matchesConditionWithStatus(rel, "Ready", "True") {
				return subroutines.StopWithRequeue(DefaultRequeueInterval, "istio-istiod Release is not ready"), nil
			}
		}

		hasProxy, pod, err := r.hasIstioProxyInjected(ctx, "platform-mesh-operator", "platform-mesh-system")
		if err != nil {
			log.Error().Err(err).Msg("Failed to check if istio-proxy is injected")
			return subroutines.OK(), err
		}
		// When running the operator locally there will never be a proxy
		if !r.cfg.IsLocal && !hasProxy {
			log.Info().Msg("Restarting operator to ensure istio-proxy is injected")
			err := r.clientInfra.Delete(ctx, pod)
			if err != nil {
				log.Error().Err(err).Msg("Failed to delete istio-proxy pod")
				return subroutines.OK(), err
			}
			// Forcing a pod restart
			os.Exit(0)
		}
	}

	// Wait for kcp release to be ready before continuing
	rootShard := &unstructured.Unstructured{}
	rootShard.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "RootShard"})
	// Wait for root shard to be ready
	err = r.clientRuntime.Get(ctx, types.NamespacedName{Name: operatorCfg.KCP.RootShardName, Namespace: operatorCfg.KCP.Namespace}, rootShard)
	if err != nil || !matchesConditionWithStatus(rootShard, "Available", "True") {
		return subroutines.StopWithRequeue(DefaultRequeueInterval, "RootShard is not ready"), nil
	}

	frontProxy := &unstructured.Unstructured{}
	frontProxy.SetGroupVersionKind(schema.GroupVersionKind{Group: "operator.kcp.io", Version: "v1alpha1", Kind: "FrontProxy"})
	// Wait for root shard to be ready
	err = r.clientRuntime.Get(ctx, types.NamespacedName{Name: operatorCfg.KCP.FrontProxyName, Namespace: operatorCfg.KCP.Namespace}, frontProxy)
	if err != nil || !matchesConditionWithStatus(frontProxy, "Available", "True") {
		return subroutines.StopWithRequeue(DefaultRequeueInterval, "FrontProxy is not ready"), nil
	}
	return webhookResult, nil
}

// templateVarsFromProfileInfra parses the infra profile and merges it with templateVars for rendering gotemplates/infra
func (r *DeploymentSubroutine) templateVarsFromProfileInfra(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh, templateVars apiextensionsv1.JSON, config *config.OperatorConfig) (map[string]any, error) {
	// Load profile from ConfigMap
	infraProfileYaml, _, err := r.loadProfileSections(ctx, inst)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to load profile from ConfigMap")
	}

	// Parse profile YAML to map
	var infraProfileMap map[string]any
	if err := yaml.Unmarshal([]byte(infraProfileYaml), &infraProfileMap); err != nil {
		return nil, errors.Wrap(err, "Failed to parse profile yaml")
	}

	// Parse templateVars JSON to map
	var templateVarsMap map[string]any
	if len(templateVars.Raw) > 0 {
		if err := json.Unmarshal(templateVars.Raw, &templateVarsMap); err != nil {
			return nil, errors.Wrap(err, "Failed to parse templateVars")
		}
	} else {
		templateVarsMap = make(map[string]any)
	}

	// Add instance-specific fields
	infraProfileMap["releaseNamespace"] = inst.Namespace
	infraProfileMap["kubeConfigEnabled"] = config.RemoteRuntime.IsEnabled()
	if config.RemoteRuntime.IsEnabled() {
		infraProfileMap["kubeConfigSecretName"] = config.RemoteRuntime.InfraSecretName
		infraProfileMap["kubeConfigSecretKey"] = config.RemoteRuntime.InfraSecretKey
	}

	// Add deploymentTechnology from profile or templateVars (defaults to fluxcd if not specified)
	deploymentTech := deploymentTechFluxCD // default
	if deploymentTechFromProfile, ok := infraProfileMap["deploymentTechnology"].(string); ok && deploymentTechFromProfile != "" {
		deploymentTech = deploymentTechFromProfile
	}
	if deploymentTechFromTemplateVars, ok := templateVarsMap["deploymentTechnology"].(string); ok && deploymentTechFromTemplateVars != "" {
		deploymentTech = deploymentTechFromTemplateVars
	}
	// Normalize to lowercase
	deploymentTech = strings.ToLower(deploymentTech)
	if deploymentTech != deploymentTechFluxCD && deploymentTech != deploymentTechArgoCD {
		deploymentTech = deploymentTechFluxCD // default to fluxcd if invalid
	}
	infraProfileMap["deploymentTechnology"] = deploymentTech

	// Merge infra profile (base) with templateVars (overrides)
	// templateVars take precedence over profile values
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())
	tmplVars, err := merge.MergeMaps(infraProfileMap, templateVarsMap, log)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to merge infra profile with templateVars")
	}

	// Ensure helmReleaseNamespace is set: prefer deploymentNamespace, then existing helmReleaseNamespace, then inst.Namespace
	tmplVars["helmReleaseNamespace"] = infraHelmReleaseNamespace(tmplVars, inst.Namespace)

	// Inject exposure-derived vars so the infra profile can use {{ .traefikClusterIP }} etc.
	for k, v := range getExposureParams(inst).templateVars(r.cfgOperator.KCP) {
		tmplVars[k] = v
	}

	// Render any {{ }} template syntax embedded in profile values (e.g. service.spec.clusterIP)
	// using the now-complete tmplVars as data.
	rendered, err := renderTemplatesInValue(tmplVars, tmplVars)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to render template vars in infra profile")
	}
	if renderedMap, ok := rendered.(map[string]any); ok {
		tmplVars = renderedMap
	}

	return tmplVars, nil
}

// buildRuntimeTemplateVars merges infra profile, templateVars, PlatformMesh.spec, and profile-components.yaml services
// for rendering gotemplates/infra/runtime templates
func (r *DeploymentSubroutine) buildRuntimeTemplateVars(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh, templateVars apiextensionsv1.JSON) (map[string]any, error) {
	log := logger.LoadLoggerFromContext(ctx)

	// Load profile from ConfigMap
	infraProfile, componentsProfile, err := r.loadProfileSections(ctx, inst)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to load profile from ConfigMap")
	}

	// Start with infra profile as base (runtime templates need infra profile data)
	var profileData map[string]any
	if err := yaml.Unmarshal([]byte(infraProfile), &profileData); err != nil {
		return nil, errors.Wrap(err, "Failed to parse infra profile for runtime templates")
	}

	// Parse templateVars JSON
	var templateVarsMap map[string]any
	if len(templateVars.Raw) > 0 {
		if err := json.Unmarshal(templateVars.Raw, &templateVarsMap); err != nil {
			return nil, errors.Wrap(err, "Failed to parse templateVars")
		}
	} else {
		templateVarsMap = make(map[string]any)
	}

	// Merge infra profile (base) with templateVars (overrides)
	baseVars, err := merge.MergeMaps(profileData, templateVarsMap, log)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to merge infra profile with templateVars")
	}

	// Merge PlatformMesh.spec.Values
	var specValues map[string]any
	if len(inst.Spec.Values.Raw) > 0 {
		if err := json.Unmarshal(inst.Spec.Values.Raw, &specValues); err != nil {
			return nil, errors.Wrap(err, "Failed to parse PlatformMesh.spec.Values")
		}
		var err error
		baseVars, err = merge.MergeMaps(baseVars, specValues, log)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to merge PlatformMesh.spec.Values")
		}
	}

	// Merge PlatformMesh.spec.OCM config
	if inst.Spec.OCM != nil {
		ocmConfig := make(map[string]any)
		if inst.Spec.OCM.Repo != nil {
			ocmConfig["repo"] = map[string]any{
				"name": inst.Spec.OCM.Repo.Name,
			}
		}
		if inst.Spec.OCM.Component != nil {
			ocmConfig["component"] = map[string]any{
				"name": inst.Spec.OCM.Component.Name,
			}
		}
		if len(inst.Spec.OCM.ReferencePath) > 0 {
			refPath := make([]any, len(inst.Spec.OCM.ReferencePath))
			for i, el := range inst.Spec.OCM.ReferencePath {
				refPath[i] = map[string]any{"name": el.Name}
			}
			ocmConfig["referencePath"] = refPath
		}
		if len(ocmConfig) > 0 {
			// Merge OCM config into existing ocm key if present
			if existingOcm, ok := baseVars["ocm"].(map[string]any); ok {
				var err error
				ocmConfig, err = merge.MergeMaps(existingOcm, ocmConfig, log)
				if err != nil {
					return nil, errors.Wrap(err, "Failed to merge OCM config")
				}
			}
			baseVars["ocm"] = ocmConfig
		}
	}

	// Get profile-components.yaml services
	// Render profile-components.yaml as a Go template with templateVars
	tmpl, err := template.New("profile-components").Funcs(templateFuncMap()).Parse(componentsProfile)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to parse profile-components.yaml template")
	}

	var buf bytes.Buffer
	// Render profile-components.yaml template with baseVars directly (not wrapped in Values)
	// This allows templates to use {{ .baseDomain }} instead of {{ .Values.baseDomain }}
	if err := tmpl.Execute(&buf, baseVars); err != nil {
		return nil, errors.Wrap(err, "Failed to execute profile-components.yaml template")
	}

	// Parse the rendered YAML
	var profileComponentsData map[string]any
	if err := yaml.Unmarshal(buf.Bytes(), &profileComponentsData); err != nil {
		return nil, errors.Wrap(err, "Failed to unmarshal rendered profile-components.yaml")
	}

	// Extract services from profile-components.yaml
	if services, ok := profileComponentsData["services"].(map[string]any); ok {
		// Merge services into baseVars
		if existingServices, ok := baseVars["services"].(map[string]any); ok {
			// Merge services from profile into existing services
			mergedServices, err := merge.MergeMaps(existingServices, services, log)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to merge services from profile-components.yaml")
			}
			baseVars["services"] = mergedServices
		} else {
			baseVars["services"] = services
		}
	}

	// Add instance-specific fields
	baseVars["releaseNamespace"] = inst.Namespace
	if deployNs, ok := baseVars["deploymentNamespace"].(string); ok && deployNs != "" {
		baseVars["helmReleaseNamespace"] = deployNs
	} else {
		baseVars["helmReleaseNamespace"] = inst.Namespace
	}
	baseVars["kubeConfigEnabled"] = r.cfgOperator.RemoteRuntime.IsEnabled()
	if r.cfgOperator.RemoteRuntime.IsEnabled() {
		baseVars["kubeConfigSecretName"] = r.cfgOperator.RemoteRuntime.InfraSecretName
		baseVars["kubeConfigSecretKey"] = r.cfgOperator.RemoteRuntime.InfraSecretKey
	}

	return baseVars, nil
}

// buildComponentsTemplateVars parses components profile using TemplateVars and produces the data
// structure expected by gotemplates/components (root keys: values, releaseNamespace).
// The second return value is the set of HelmRelease keys ("namespace/name") that are disabled and meant to be removed.
func (r *DeploymentSubroutine) buildComponentsTemplateVars(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh, templateVars apiextensionsv1.JSON) (map[string]any, map[string]struct{}, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	// The infra section is needed too: services may depend on infra HelmReleases, which
	// carry their own enabled flags.
	infraProfileYaml, componentsProfileYaml, err := r.loadProfileSections(ctx, inst)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to load profile from ConfigMap")
	}

	// Parse components profile as YAML to get the base structure
	var componentsProfileMap map[string]any
	if err := yaml.Unmarshal([]byte(componentsProfileYaml), &componentsProfileMap); err != nil {
		return nil, nil, errors.Wrap(err, "Failed to parse components profile as YAML")
	}

	var infraProfileMap map[string]any
	if err := yaml.Unmarshal([]byte(infraProfileYaml), &infraProfileMap); err != nil {
		return nil, nil, errors.Wrap(err, "Failed to parse infra profile as YAML")
	}

	exposure := getExposureParams(inst)
	exposureTemplateVars := exposure.templateVars(r.cfgOperator.KCP)

	// Parse templateVars JSON into a map
	var templateVarsMap map[string]any
	if len(templateVars.Raw) > 0 {
		if err := json.Unmarshal(templateVars.Raw, &templateVarsMap); err != nil {
			return nil, nil, errors.Wrap(err, "Failed to unmarshal templateVars for components profile")
		}
	} else {
		templateVarsMap = make(map[string]any)
	}

	// Resolve the infra namespace the same way templateVarsFromProfileInfra does. Must happen
	// before templateVarsMap is merged with the components profile below, otherwise component
	// keys leak into the infra resolution.
	infraVars, err := merge.MergeMaps(infraProfileMap, templateVarsMap, log)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to merge infra profile with templateVars")
	}
	infraNamespace := infraHelmReleaseNamespace(infraVars, inst.Namespace)

	// Merge components profile (base) with templateVars (overrides)
	// templateVars take precedence over profile values
	templateVarsMap, err = merge.MergeMaps(componentsProfileMap, templateVarsMap, log)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to merge profile-components.yaml with templateVars")
	}

	// Inject exposure-derived vars so profile templates like {{ .traefikClusterIP }} render correctly.
	// These are added after the templateVars merge so they can't be overridden by the profile config,
	// but they are available when the profile YAML itself is rendered as a template.
	{
		for k, v := range exposureTemplateVars {
			templateVarsMap[k] = v
		}
	}

	// Render profile-components.yaml as a Go template with tv directly (merged values)
	// Templates can use {{ .baseDomain }} instead of {{ .Values.baseDomain }}
	tmpl, err := template.New("profile-components").Funcs(templateFuncMap()).Parse(componentsProfileYaml)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to parse profile-components.yaml template")
	}

	var buf bytes.Buffer
	// Render profile-components.yaml template with tv directly (not wrapped in Values)
	// This allows templates to use {{ .baseDomain }} instead of {{ .Values.baseDomain }}
	if err := tmpl.Execute(&buf, templateVarsMap); err != nil {
		return nil, nil, errors.Wrap(err, "Failed to execute profile-components.yaml template")
	}

	// Now parse the rendered YAML into a generic values map
	values := map[string]any{}
	if err := yaml.Unmarshal(buf.Bytes(), &values); err != nil {
		return nil, nil, errors.Wrap(err, "Failed to unmarshal rendered profile-components.yaml")
	}

	// Extract services from the rendered profile-components.yaml
	var baseServices map[string]any
	if services, ok := values["services"].(map[string]any); ok {
		baseServices = services
	} else {
		baseServices = make(map[string]any)
	}

	// Build template data for rendering templates in spec.Values
	templateData := make(map[string]any)
	for k, v := range exposureTemplateVars {
		templateData[k] = v
	}
	templateData["baseDomainWithPort"] = exposure.baseDomainWithPort()

	// Extract services from PlatformMesh.spec.Values
	// spec.Values can either have services under a "services" key, or the entire spec.Values can be services
	var specServices map[string]any
	if len(inst.Spec.Values.Raw) > 0 {
		var specValues map[string]any
		if err := json.Unmarshal(inst.Spec.Values.Raw, &specValues); err != nil {
			return nil, nil, errors.Wrap(err, "Failed to parse PlatformMesh.spec.Values")
		}
		// Check if services are under a "services" key
		if services, ok := specValues["services"].(map[string]any); ok {
			specServices = services
		} else {
			// If no "services" key, treat the entire specValues as services (flat structure)
			// This matches the behavior in MergeValuesAndServices
			specServices = specValues
		}

		// Render any template syntax in specServices before merging
		// Wrap templateData in Values key to support {{ .Values.* }} syntax in spec.Values
		wrappedTemplateData := map[string]any{
			"Values": templateData,
		}
		// Also add top-level keys for backward compatibility with {{ .baseDomain }} syntax
		for k, v := range templateData {
			wrappedTemplateData[k] = v
		}
		renderedServices, err := renderTemplatesInValue(specServices, wrappedTemplateData)
		if err != nil {
			return nil, nil, errors.Wrap(err, "Failed to render templates in PlatformMesh.spec.Values services")
		}
		if renderedMap, ok := renderedServices.(map[string]any); ok {
			specServices = renderedMap
		}
	}

	// Deep merge specServices into baseServices (specServices takes precedence)
	mergedServices, err := merge.MergeMaps(baseServices, specServices, log)
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to merge services from PlatformMesh.spec.Values with profile-components.yaml services")
	}

	// deploymentNamespace: where deployment CRs live (configurable via profile)
	componentsNamespace := inst.Namespace
	if deployNs, ok := values["deploymentNamespace"].(string); ok && deployNs != "" {
		componentsNamespace = deployNs
	}

	// Drop dependencies on switched-off components: they are never rendered, so Flux would
	// hold the dependent release in DependencyNotReady forever. Also keeps calculateSyncWaves
	// below from ordering against a service that does not exist.
	disabled := disabledHelmReleases(infraProfileMap, mergedServices, infraNamespace, componentsNamespace)
	pruneDisabledDependencies(mergedServices, disabled, componentsNamespace, log)

	// Put the merged services back into values
	values["services"] = mergedServices

	// Root data passed to component gotemplates
	data := map[string]any{
		"values":              values,
		"releaseNamespace":    inst.Namespace,
		"deploymentNamespace": componentsNamespace,
	}

	// Add kubeConfig fields for remote PlatformMesh support
	data["kubeConfigEnabled"] = r.cfgOperator.RemoteRuntime.IsEnabled()
	if r.cfgOperator.RemoteRuntime.IsEnabled() {
		data["kubeConfigSecretName"] = r.cfgOperator.RemoteRuntime.InfraSecretName
		data["kubeConfigSecretKey"] = r.cfgOperator.RemoteRuntime.InfraSecretKey
	}

	// Add deploymentTechnology from profile or templateVars (defaults to fluxcd if not specified)
	deploymentTech := deploymentTechFluxCD // default
	if deploymentTechFromProfile, ok := values["deploymentTechnology"].(string); ok && deploymentTechFromProfile != "" {
		deploymentTech = deploymentTechFromProfile
	}
	if deploymentTechFromTemplateVars, ok := templateVarsMap["deploymentTechnology"].(string); ok && deploymentTechFromTemplateVars != "" {
		deploymentTech = deploymentTechFromTemplateVars
	}
	// Normalize to lowercase
	deploymentTech = strings.ToLower(deploymentTech)
	if deploymentTech != deploymentTechFluxCD && deploymentTech != deploymentTechArgoCD {
		deploymentTech = deploymentTechFluxCD // default to fluxcd if invalid
	}
	data["deploymentTechnology"] = deploymentTech

	// Calculate sync waves for ArgoCD Applications based on dependsOn
	if deploymentTech == deploymentTechArgoCD {
		if err := calculateSyncWaves(mergedServices); err != nil {
			log.Warn().Err(err).Msg("Failed to calculate sync waves, continuing without sync wave annotations")
		}
	}

	// Extract destinationServer from components profile and add it to root level for template access
	if destinationServer, ok := values["destinationServer"].(string); ok && destinationServer != "" {
		data["destinationServer"] = destinationServer
	}

	for k, v := range exposureTemplateVars {
		data[k] = v
	}
	data["baseDomainWithPort"] = exposure.baseDomainWithPort()

	return data, disabled, nil
}

// calculateSyncWaves calculates ArgoCD sync waves based on dependsOn relationships
// Services with no dependencies get wave 0, services depending on wave N get wave N+1
func calculateSyncWaves(services map[string]any) error {
	if services == nil {
		return nil
	}

	// Build dependency graph: service -> list of dependencies
	dependencies := make(map[string][]string)
	serviceNames := make([]string, 0)

	// First pass: collect all services and their dependencies
	for serviceName, serviceConfig := range services {
		serviceStr := serviceName
		serviceNames = append(serviceNames, serviceStr)
		dependencies[serviceStr] = []string{}

		config, _ := serviceConfig.(map[string]any)

		// Check if service has dependsOn
		dependsOn, found := config["dependsOn"]
		if !found {
			continue
		}

		// dependsOn can be a slice of maps with "name" and optional "namespace"
		dependsOnSlice, _ := dependsOn.([]any)

		for _, dep := range dependsOnSlice {
			depMap, ok := dep.(map[string]any)
			if !ok {
				continue
			}

			depName, ok := depMap["name"].(string)
			if !ok || depName == "" {
				continue
			}

			// Add dependency (using service name as-is, assuming same namespace unless specified)
			dependencies[serviceStr] = append(dependencies[serviceStr], depName)
		}
	}

	// First, collect user-configured syncWave values (from profile or PlatformMesh.spec.Values)
	// These should be preserved and not overwritten by automatic calculation
	userConfiguredSyncWaves := make(map[string]int)
	for serviceName, serviceConfig := range services {
		config, _ := serviceConfig.(map[string]any)

		// Check if syncWave is already configured by the user
		if syncWaveVal, found := config["syncWave"]; found {
			switch v := syncWaveVal.(type) {
			case int:
				userConfiguredSyncWaves[serviceName] = v
			case int64:
				userConfiguredSyncWaves[serviceName] = int(v)
			case float64:
				// JSON numbers unmarshal as float64
				userConfiguredSyncWaves[serviceName] = int(v)
			}
		}
	}

	// Calculate sync waves using iterative approach
	// Services with no dependencies get wave 0
	// Services depending on wave N services get wave N+1
	syncWaves := make(map[string]int)

	// Initialize all services to wave 0, or use user-configured value if present
	for _, serviceName := range serviceNames {
		if userWave, exists := userConfiguredSyncWaves[serviceName]; exists {
			syncWaves[serviceName] = userWave
		} else {
			syncWaves[serviceName] = 0
		}
	}

	// Calculate waves iteratively until no changes (handles dependencies)
	// Maximum iterations to prevent infinite loops (should be <= number of services)
	maxIterations := len(serviceNames) + 1
	for range maxIterations {
		changed := false
		for _, serviceName := range serviceNames {
			deps := dependencies[serviceName]
			if len(deps) == 0 {
				// No dependencies, stays at wave 0
				continue
			}

			// Find max wave of all valid dependencies
			maxDepWave := -1
			hasValidDeps := false
			for _, depName := range deps {
				depWave, depExists := syncWaves[depName]
				if !depExists {
					// Dependency not found in services, ignore it
					continue
				}
				hasValidDeps = true
				if depWave > maxDepWave {
					maxDepWave = depWave
				}
			}

			if hasValidDeps {
				// Set this service's wave to max dependency wave + 1
				newWave := maxDepWave + 1
				// Only update if not user-configured (preserve user-configured values)
				// Dependencies will still be respected because we use syncWaves (which includes user values) for dependency calculation
				if _, isUserConfigured := userConfiguredSyncWaves[serviceName]; !isUserConfigured {
					currentWave := syncWaves[serviceName]
					if currentWave < newWave {
						syncWaves[serviceName] = newWave
						changed = true
					}
				}
			}
		}

		if !changed {
			// No more changes, we're done
			break
		}
	}

	// Add sync wave to each service config
	// Only overwrite if not user-configured, otherwise preserve user value
	// Note: ignoreDifferences is also preserved from the profile components section
	// and will be available in the config for use in templates
	for serviceName, serviceConfig := range services {
		config, ok := serviceConfig.(map[string]any)
		if !ok || config == nil {
			// Skip non-map service configs
			continue
		}
		if _, isUserConfigured := userConfiguredSyncWaves[serviceName]; isUserConfigured {
			// Keep user-configured value, but still respect dependencies if needed
			// For user-configured values, we keep them as-is
			continue
		}

		wave, exists := syncWaves[serviceName]
		if !exists {
			// Service not in syncWaves map, default to wave 0
			wave = 0
		}

		// Set syncWave field (only if not user-configured)
		config["syncWave"] = wave

		// ignoreDifferences is preserved from the profile components section
		// It is already in the config map from the merged services, so no explicit
		// handling is needed here - it will be available in templates via $config.ignoreDifferences
	}

	return nil
}

// renderTemplatesInValue recursively traverses a value (map, slice, or string) and renders
// any Go text/template expressions found in string values. Non-string values pass through unchanged.
//
// Template expressions use standard Go template syntax with access to templateData via dot notation.
// If a string does not contain "{{" and "}}", it is returned as-is. If template parsing or execution
// fails, the original string is returned without error to handle intentional non-template braces.
//
// Examples:
//
//	templateData := map[string]interface{}{
//	    "namespace": "prod",
//	    "config": map[string]interface{}{
//	        "clusterName": "cluster-1",
//	        "syncWave":    "10",
//	    },
//	}
//
//	// String with template → rendered
//	renderTemplatesInValue("ns-{{ .namespace }}", templateData)
//	// Returns: "ns-prod", nil
//
//	// Nested map access
//	renderTemplatesInValue("wave-{{ .config.syncWave }}", templateData)
//	// Returns: "wave-10", nil
//
//	// Map values are recursively rendered
//	renderTemplatesInValue(map[string]interface{}{
//	    "name": "{{ .config.clusterName }}",
//	    "port": 8080,
//	}, templateData)
//	// Returns: map[string]interface{}{"name": "cluster-1", "port": 8080}, nil
//
//	// Slice values are recursively rendered
//	renderTemplatesInValue([]interface{}{"{{ .namespace }}", "static"}, templateData)
//	// Returns: []interface{}{"prod", "static"}, nil
//
//	// Non-template strings pass through
//	renderTemplatesInValue("plain-text", templateData)
//	// Returns: "plain-text", nil
//
//	// Non-string types pass through unchanged
//	renderTemplatesInValue(42, templateData)
//	// Returns: 42, nil
func renderTemplatesInValue(v any, templateData map[string]any) (any, error) {
	switch val := v.(type) {
	case map[string]any:
		// Create a copy to avoid modifying the original during iteration
		result := make(map[string]any)
		for k, item := range val {
			rendered, err := renderTemplatesInValue(item, templateData)
			if err != nil {
				return nil, err
			}
			result[k] = rendered
		}
		return result, nil
	case []any:
		// Create a new slice with rendered values
		result := make([]any, len(val))
		for i, item := range val {
			rendered, err := renderTemplatesInValue(item, templateData)
			if err != nil {
				return nil, err
			}
			result[i] = rendered
		}
		return result, nil
	case string:
		// Check if the string contains template syntax
		if strings.Contains(val, "{{") && strings.Contains(val, "}}") {
			// Parse and render the template
			parsed, err := template.New("value").Funcs(templateFuncMap()).Parse(val)
			if err != nil {
				// If parsing fails, it might not be a valid template, so return the original value
				return val, nil //nolint:nilerr
			}
			var buf bytes.Buffer
			if err := parsed.Execute(&buf, templateData); err != nil {
				// If execution fails, return the original value (don't error, might be intentional)
				return val, nil //nolint:nilerr
			}
			return buf.String(), nil
		}
		return val, nil
	default:
		return val, nil
	}
}

// mergeImageVersionsIntoHelmValues reads the ImageVersionStore for the given Application and
// merges any Resource-managed image versions into the object's spec.source.helm.values YAML string.
func (r *DeploymentSubroutine) mergeImageVersionsIntoHelmValues(obj map[string]any, appName, namespace string, log *logger.Logger) {
	if r.imageVersionStore == nil {
		return
	}
	versions := r.imageVersionStore.Get(namespace, appName)
	if len(versions) == 0 {
		return
	}

	spec, _ := obj["spec"].(map[string]any)
	source, _ := spec["source"].(map[string]any)
	helm, _ := source["helm"].(map[string]any)
	if helm == nil {
		helm = map[string]any{}
		source["helm"] = helm
	}
	valuesStr, _ := helm["values"].(string)

	updatedYAML, err := SetHelmValues(valuesStr, versions)
	if err != nil {
		log.Warn().Err(err).Str("application", appName).Msg("Failed to merge image versions into helm values")
		return
	}

	for _, iv := range versions {
		log.Debug().Str("application", appName).Str("path", iv.Path).Str("version", iv.Version).Msg("Merged Resource image version into helm values")
	}
	helm["values"] = updatedYAML
}

// mergeImageVersionsIntoHelmReleaseValues reads the ImageVersionStore for the given HelmRelease and
// merges any Resource-managed image versions into the object's spec.values structured field.
// If the store records that the HelmRelease was unsuspended by ResourceSubroutine, it also
// overrides spec.suspend to false so SSA does not re-apply suspend: true from the template.
func (r *DeploymentSubroutine) mergeImageVersionsIntoHelmReleaseValues(obj *unstructured.Unstructured, releaseName, namespace string, log *logger.Logger) {
	if r.imageVersionStore == nil {
		return
	}
	versions := r.imageVersionStore.Get(namespace, releaseName)
	if len(versions) == 0 {
		return
	}

	for _, iv := range versions {
		pathElems := SplitPath(iv.Path)
		fullPath := append([]string{"spec", "values"}, pathElems...)
		if err := unstructured.SetNestedField(obj.Object, iv.Version, fullPath...); err != nil {
			log.Warn().Err(err).Str("helmRelease", releaseName).Str("path", iv.Path).Msg("Failed to merge image version into HelmRelease spec.values")
			continue
		}
		log.Debug().Str("helmRelease", releaseName).Str("path", iv.Path).Str("version", iv.Version).Msg("Merged Resource image version into HelmRelease spec.values")
	}

	if r.imageVersionStore.IsUnsuspended(namespace, releaseName) {
		renderedSuspend, found, _ := unstructured.NestedBool(obj.Object, "spec", "suspend")
		if found && renderedSuspend {
			_ = unstructured.SetNestedField(obj.Object, false, "spec", "suspend")
			log.Debug().Str("helmRelease", releaseName).Msg("Overriding suspend: true in template — HelmRelease was unsuspended by ResourceSubroutine")
		}
	}
}

// renderAndApplyInfraTemplates renders all templates in gotemplates/infra/infra and applies them.
func (r *DeploymentSubroutine) renderAndApplyInfraTemplates(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh, templateVars apiextensionsv1.JSON) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	tmplVars, err := r.templateVarsFromProfileInfra(ctx, inst, templateVars, r.cfgOperator)
	if err != nil {
		log.Error().Err(err).Msg("Failed to build template variables from profile")
		return err
	}

	deploymentTech, _ := tmplVars["deploymentTechnology"].(string)
	deploymentTech = strings.ToLower(deploymentTech)

	skipFile := deploymentTechFileFilter(deploymentTech, log)
	postProcess := r.infraManifestPostProcess(ctx, log)

	return r.renderAndApplyTemplates(ctx, r.gotemplatesInfraDir+"/infra", tmplVars, r.clientInfra, log, "infra", skipFile, postProcess)
}

// renderAndApplyRuntimeTemplates renders all templates in gotemplates/infra/runtime and applies them.
// The directory contains two types of objects:
//   - OCM Resource objects (delivery.ocm.software/v1alpha1) → applied to the runtime cluster, where
//     the OCM controller processes them and updates their status (version, imageReference, etc.)
//   - FluxCD HelmRelease objects (helm.toolkit.fluxcd.io) → applied to the infra cluster, where
//     FluxCD runs and deploys workloads to the runtime cluster via kubeConfig
//
// In single-cluster deployments clientInfra == clientRuntime so the routing is transparent.
func (r *DeploymentSubroutine) renderAndApplyRuntimeTemplates(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh, templateVars apiextensionsv1.JSON) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	tmplVars, err := r.buildRuntimeTemplateVars(ctx, inst, templateVars)
	if err != nil {
		log.Error().Err(err).Msg("Failed to build template variables for runtime templates")
		return err
	}

	// Route each rendered object to the correct cluster client based on its GVK.
	// OCM Resources → runtime cluster (OCM controller lives there).
	// Everything else (FluxCD HelmReleases, etc.) → infra cluster.
	routingPostProcess := func(ctx context.Context, obj *unstructured.Unstructured) error {
		targetClient := r.clientInfra
		if obj.GetAPIVersion() == "delivery.ocm.software/v1alpha1" && obj.GetKind() == "Resource" {
			targetClient = r.clientRuntime
		}
		return targetClient.Patch(ctx, obj, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(fieldManagerDeployment), ctrlruntimeclient.ForceOwnership) //nolint:staticcheck // Apply via Patch is required for unstructured objects
	}

	// Use clientInfra as default (it will be overridden per-object by routingPostProcess).
	// We pass a no-op postProcessObj and handle the actual Apply inside routingPostProcess.
	return r.renderAndApplyTemplatesWithRouter(ctx, r.gotemplatesInfraDir+"/runtime", tmplVars, log, "runtime", nil, routingPostProcess)
}

// renderAndApplyComponentsInfraTemplates renders gotemplates/components/infra with profile-components.yaml
// and applies the resulting manifests to the infra cluster.
func (r *DeploymentSubroutine) renderAndApplyComponentsInfraTemplates(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh, templateVars apiextensionsv1.JSON) (map[string]struct{}, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	tmplVars, disabled, err := r.buildComponentsTemplateVars(ctx, inst, templateVars)
	if err != nil {
		log.Error().Err(err).Msg("Failed to build components template data for infra")
		return nil, err
	}

	deploymentTech, _ := tmplVars["deploymentTechnology"].(string)
	deploymentTech = strings.ToLower(deploymentTech)

	skipFile := deploymentTechFileFilter(deploymentTech, log)
	postProcess := r.infraManifestPostProcess(ctx, log)

	return disabled, r.renderAndApplyTemplates(ctx, r.gotemplatesComponentsDir+"/infra", tmplVars, r.clientInfra, log, "components-infra", skipFile, postProcess)
}

// renderAndApplyComponentsRuntimeTemplates renders gotemplates/components/runtime with profile-components.yaml
// and applies the resulting manifests to the runtime cluster (OCM Resources).
func (r *DeploymentSubroutine) renderAndApplyComponentsRuntimeTemplates(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh, templateVars apiextensionsv1.JSON) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	tmplVars, _, err := r.buildComponentsTemplateVars(ctx, inst, templateVars)
	if err != nil {
		log.Error().Err(err).Msg("Failed to build components template data for runtime")
		return err
	}

	return r.renderAndApplyTemplates(ctx, r.gotemplatesComponentsDir+"/runtime", tmplVars, r.clientRuntime, log, "components-runtime", nil, nil)
}

func mergeOCMConfig(mapValues map[string]any, inst *pmcorev1alpha1.PlatformMesh) {
	if inst.Spec.OCM != nil {
		repoConfig := map[string]any{}
		compConfig := map[string]any{}

		if inst.Spec.OCM.Repo != nil {
			repoConfig = map[string]any{
				"name": inst.Spec.OCM.Repo.Name,
			}
		}

		if inst.Spec.OCM.Component != nil {
			compConfig = map[string]any{
				"name": inst.Spec.OCM.Component.Name,
			}
		}
		referencePath := make([]any, 0, len(inst.Spec.OCM.ReferencePath))
		for _, element := range inst.Spec.OCM.ReferencePath {
			referencePath = append(referencePath, map[string]any{"name": element.Name})
		}
		ocmConfig := map[string]any{
			"repo":          repoConfig,
			"component":     compConfig,
			"referencePath": referencePath,
		}
		mapValues["ocm"] = ocmConfig
	}
}

func (r *DeploymentSubroutine) createKCPWebhookSecret(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh) error {
	log := logger.LoadLoggerFromContext(ctx)
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)
	webhookSecret := operatorCfg.Subroutines.Deployment.AuthorizationWebhookSecretName
	_, err := GetSecret(r.clientRuntime, webhookSecret, inst.Namespace)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error().Err(err).Str("secret", webhookSecret).Str("namespace", inst.Namespace).Msg("Failed to get kcp webhook secret")
		return err
	}
	if err == nil {
		return nil
	}

	// Continue to create the secret
	obj, err := unstructuredFromFile(fmt.Sprintf("%s/rebac-auth-webhook/kcp-webhook-secret.yaml", r.workspaceDirectory), map[string]any{}, log)
	if err != nil {
		return err
	}
	obj.SetNamespace(inst.Namespace)

	// Apply the secret using SSA (idempotent - creates if not exists, updates if exists)
	if err := r.clientRuntime.Patch(ctx, &obj, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(fieldManagerDeployment), ctrlruntimeclient.ForceOwnership); err != nil { //nolint:staticcheck // Apply via Patch is required for unstructured objects
		return err
	}
	return nil
}

func (r *DeploymentSubroutine) updateKcpWebhookSecret(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx)
	operatorCfg := pmconfig.LoadConfigFromContext(ctx).(config.OperatorConfig)

	// Retrieve the ca.crt from the rebac-authz-webhook-cert secret
	caSecretName := operatorCfg.Subroutines.Deployment.AuthorizationWebhookSecretCAName
	webhookCertSecret, err := GetSecret(r.clientRuntime, caSecretName, inst.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info().Str("name", caSecretName).Msg("Webhook secret does not exist")
			return subroutines.OKWithRequeue(DefaultRequeueInterval), nil
		}
		log.Error().Err(err).Str("secret", caSecretName).Str("namespace", inst.Namespace).Msg("Failed to get webhook cert secret")
		return subroutines.OK(), err
	}

	caCrt, ok := webhookCertSecret.Data["ca.crt"]
	if !ok || len(caCrt) == 0 {
		err := fmt.Errorf("ca.crt not found or empty in secret %s/%s", inst.Namespace, caSecretName)
		log.Error().Err(err).Msg("ca.crt missing from webhook cert secret")
		return subroutines.OK(), err
	}

	// A self-signed certificate is left over from before the CA issuer and is about to be
	// reissued, so check again soon to sync the new CA.
	issuedResult := subroutines.OK()
	if bytes.Equal(caCrt, webhookCertSecret.Data["tls.crt"]) {
		log.Info().Str("name", caSecretName).Msg("Webhook certificate is not yet issued by the CA issuer")
		issuedResult = subroutines.OKWithRequeue(DefaultRequeueInterval)
	}

	// Get the kcp-webhook-secret
	webhookSecret := operatorCfg.Subroutines.Deployment.AuthorizationWebhookSecretName
	kcpWebhookSecret, err := GetSecret(r.clientRuntime, webhookSecret, inst.Namespace)
	if err != nil {
		log.Error().Err(err).Str("secret", webhookSecret).Str("namespace", inst.Namespace).Msg("Failed to get kcp webhook secret")
		return subroutines.OK(), err
	}

	// Get the kubeconfig from the secret
	kubeconfigData, ok := kcpWebhookSecret.Data["kubeconfig"]
	if !ok || len(kubeconfigData) == 0 {
		err := fmt.Errorf("kubeconfig not found or empty in secret %s/%s", inst.Namespace, webhookSecret)
		log.Error().Err(err).Msg("kubeconfig missing from kcp webhook secret")
		return subroutines.OK(), err
	}

	// Parse the kubeconfig using clientcmd utilities
	kubeconfig, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load kubeconfig")
		return subroutines.OK(), err
	}

	// Update the certificate-authority-data in all clusters only if it actually changed
	updated := false
	for clusterName, cluster := range kubeconfig.Clusters {
		if cluster != nil && !bytes.Equal(cluster.CertificateAuthorityData, caCrt) {
			cluster.CertificateAuthorityData = caCrt
			kubeconfig.Clusters[clusterName] = cluster
			updated = true
			log.Debug().Str("cluster", clusterName).Msg("Updated certificate-authority-data in cluster")
		}
	}

	if !updated {
		log.Debug().Msg("certificate-authority-data is already up to date in kcp-webhook-secret, skipping update")
		return issuedResult, nil
	}

	// Marshal the updated kubeconfig back to YAML using clientcmd
	updatedKubeconfigData, err := clientcmd.Write(*kubeconfig)
	if err != nil {
		log.Error().Err(err).Msg("Failed to write updated kubeconfig")
		return subroutines.OK(), err
	}

	// Update the secret with the new kubeconfig
	kcpWebhookSecret.Data["kubeconfig"] = updatedKubeconfigData

	// Clear managedFields before applying with SSA (required for SSA)
	kcpWebhookSecret.SetManagedFields(nil)

	// Apply the updated secret using SSA
	err = r.clientRuntime.Patch(ctx, kcpWebhookSecret, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(fieldManagerDeployment), ctrlruntimeclient.ForceOwnership) //nolint:staticcheck // Apply via Patch is required for unstructured objects
	if err != nil {
		log.Error().Err(err).Str("secret", webhookSecret).Str("namespace", operatorCfg.KCP.Namespace).Msg("Failed to update kcp webhook secret")
		return subroutines.OK(), err
	}

	log.Info().Str("secret", webhookSecret).Str("namespace", operatorCfg.KCP.Namespace).Msg("Successfully updated kcp webhook secret with new certificate-authority-data")

	// Delete all kcp pods so they pick up the new webhook secret
	log.Info().Msg("kcp-webhook-secret was updated, deleting kcp pods to pick up new certificate-authority-data")
	if oErr := r.deleteKcpPods(ctx, operatorCfg.KCP.Namespace); oErr != nil {
		return subroutines.OK(), oErr
	}

	return issuedResult, nil
}

// deleteKcpPods deletes all pods with label app.kubernetes.io/name=kcp in the given namespace
// so they restart and pick up updated secrets.
func (r *DeploymentSubroutine) deleteKcpPods(ctx context.Context, namespace string) error {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(labels.Set{"app.kubernetes.io/name": "kcp"})
	if err := r.clientRuntime.List(ctx, podList, &ctrlruntimeclient.ListOptions{
		LabelSelector: labelSelector,
		Namespace:     namespace,
	}); err != nil {
		log.Error().Err(err).Str("namespace", namespace).Msg("Failed to list kcp pods")
		return err
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		log.Info().Str("pod", pod.Name).Str("namespace", pod.Namespace).Msg("Deleting kcp pod")
		if err := r.clientRuntime.Delete(ctx, pod); err != nil {
			if !apierrors.IsNotFound(err) {
				log.Error().Err(err).Str("pod", pod.Name).Msg("Failed to delete kcp pod")
				return err
			}
		}
	}

	log.Info().Int("count", len(podList.Items)).Str("namespace", namespace).Msg("Deleted kcp pods")
	return nil
}

func getHelmRelease(ctx context.Context, client ctrlruntimeclient.Client, releaseName string, releaseNamespace string) (*unstructured.Unstructured, error) {
	kcpRelease := &unstructured.Unstructured{}
	kcpRelease.SetGroupVersionKind(schema.GroupVersionKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"})
	err := client.Get(ctx, types.NamespacedName{Name: releaseName, Namespace: releaseNamespace}, kcpRelease)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info().Msgf("%s/%s Release not found, waiting for it to be created", releaseName, releaseNamespace)
			return nil, nil
		}
		log.Error().Err(err).Msgf("Failed to get %s/%s Release", releaseName, releaseNamespace)
		return nil, err
	}
	return kcpRelease, nil
}

// getDeploymentResource gets either a FluxCD HelmRelease or ArgoCD Application based on deploymentTechnology
func getDeploymentResource(ctx context.Context, client ctrlruntimeclient.Client, resourceName string, resourceNamespace string, deploymentTech string) (*unstructured.Unstructured, error) {
	deploymentTech = strings.ToLower(deploymentTech)
	obj := &unstructured.Unstructured{}

	if deploymentTech == deploymentTechArgoCD {
		obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"})
	} else {
		// Default to FluxCD
		obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"})
	}

	err := client.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, obj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info().Str("deploymentTechnology", deploymentTech).Msgf("%s/%s resource not found, waiting for it to be created", resourceName, resourceNamespace)
			return nil, fmt.Errorf("%s/%s resource not found, waiting for it to be created", resourceName, resourceNamespace)
		}
		log.Error().Err(err).Str("deploymentTechnology", deploymentTech).Msgf("Failed to get %s/%s resource", resourceName, resourceNamespace)
		return nil, err
	}
	return obj, nil
}

func isCRDEstablished(ctx context.Context, c ctrlruntimeclient.Client, name string) (bool, error) {
	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	})
	if err := c.Get(ctx, types.NamespacedName{Name: name}, crd); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return matchesConditionWithStatus(crd, "Established", "True"), nil
}

func (r *DeploymentSubroutine) hasIstioProxyInjected(ctx context.Context, labelSelector, namespace string) (bool, *unstructured.Unstructured, error) {
	pods := &unstructured.UnstructuredList{}
	pods.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"})
	err := r.clientInfra.List(ctx, pods, &ctrlruntimeclient.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{"app": labelSelector}),
		Namespace:     namespace,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to list pods with label selector: " + labelSelector)
		return false, nil, err
	}

	if len(pods.Items) > 0 {
		pod := pods.Items[0]
		spec, _ := pod.Object["spec"].(map[string]any)
		// It is possible to have istio-proxy as an initContainer or a regular container
		if initContainersInt, ok := spec["initContainers"]; ok {
			initContainers, _ := initContainersInt.([]any)
			log.Debug().Str("pod", pod.GetName()).Msgf("Found %d initContainers in pod", len(initContainers))
			for _, container := range initContainers {
				containerMap, _ := container.(map[string]any)
				name, _ := containerMap["name"].(string)
				log.Debug().Msgf("Container name: %s", name)
				if name == "istio-proxy" {
					log.Info().Msgf("Found Istio proxy container: %s", containerMap["image"])
					return true, &pod, nil
				}
			}
		}
		if containersInt, ok := spec["containers"]; ok {
			containers, _ := containersInt.([]any)
			log.Debug().Str("pod", pod.GetName()).Msgf("Found %d containers in pod", len(containers))
			for _, container := range containers {
				containerMap, _ := container.(map[string]any)
				name, _ := containerMap["name"].(string)
				log.Debug().Msgf("Container name: %s", name)
				if name == "istio-proxy" {
					log.Info().Msgf("Found Istio proxy container: %s", containerMap["image"])
					return true, &pod, nil
				}
			}
		}
		log.Info().Msgf("Istio proxy containers not found")
		return false, &pod, nil
	}

	return false, nil, errors.New("pod not found")
}

func (r *DeploymentSubroutine) manageAuthorizationWebhookSecrets(ctx context.Context, inst *pmcorev1alpha1.PlatformMesh) (subroutines.Result, error) {
	// The serving certificate is signed by a long-lived CA, so renewing it keeps the CA that kcp trusts.
	for _, file := range []string{"selfsigned-issuer.yaml", "ca-cert.yaml", "ca-issuer.yaml", "webhook-cert.yaml"} {
		path := fmt.Sprintf("%s/rebac-auth-webhook/%s", r.workspaceDirectory, file)
		if err := r.ApplyManifestFromFileWithMergedValues(ctx, path, r.clientRuntime, map[string]any{}); err != nil {
			return subroutines.OK(), err
		}
	}

	// Prepare KCP Webhook secret
	oErr := r.createKCPWebhookSecret(ctx, inst)
	if oErr != nil {
		return subroutines.OK(), oErr
	}

	// Update KCP Webhook secret with the latest CA bundle
	return r.updateKcpWebhookSecret(ctx, inst)
}

func applyManifestFromFileWithMergedValues(ctx context.Context, path string, k8sClient ctrlruntimeclient.Client, templateData map[string]any) error {
	log := logger.LoadLoggerFromContext(ctx)

	obj, err := unstructuredFromFile(path, templateData, log)
	if err != nil {
		return err
	}

	err = k8sClient.Patch(ctx, &obj, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(fieldManagerDeployment), ctrlruntimeclient.ForceOwnership) //nolint:staticcheck // Apply via Patch is required for unstructured objects
	if err != nil {
		return errors.Wrap(err, "Failed to apply manifest file: %s (%s/%s)", path, obj.GetKind(), obj.GetName())
	}
	return nil
}
