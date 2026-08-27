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

package resource

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/internal/metrics"
	"go.platform-mesh.io/platform-mesh-operator/internal/ociref"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines"
	subroutineslib "go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	requeueShort        = 5 * time.Second
	profileConfigMapKey = "profile.yaml"
)

var profileConfigMapNames = []string{"platform-mesh-profile", "platform-mesh-system-profile"}

var ociRepoGvk = schema.GroupVersionKind{
	Group:   "source.toolkit.fluxcd.io",
	Version: "v1",
	Kind:    "OCIRepository",
}

var gitRepoGvk = schema.GroupVersionKind{
	Group:   "source.toolkit.fluxcd.io",
	Version: "v1",
	Kind:    "GitRepository",
}

var helmRepoGvk = schema.GroupVersionKind{
	Group:   "source.toolkit.fluxcd.io",
	Version: "v1",
	Kind:    "HelmRepository",
}

var helmReleaseGvk = schema.GroupVersionKind{
	Group:   "helm.toolkit.fluxcd.io",
	Version: "v2",
	Kind:    "HelmRelease",
}

var argocdApplicationGvk = schema.GroupVersionKind{
	Group:   "argoproj.io",
	Version: "v1alpha1",
	Kind:    "Application",
}

var resourceFieldManager = "platform-mesh-resource"

type ResourceSubroutine struct {
	client            ctrlruntimeclient.Client // infra client for creating FluxCD resources
	clientRuntime     ctrlruntimeclient.Client // runtime client for reading profile ConfigMaps
	cfg               *config.OperatorConfig
	imageVersionStore *subroutines.ImageVersionStore
}

func NewResourceSubroutine(client ctrlruntimeclient.Client, cfg *config.OperatorConfig, imageVersionStore *subroutines.ImageVersionStore) *ResourceSubroutine {
	return &ResourceSubroutine{client: client, clientRuntime: client, cfg: cfg, imageVersionStore: imageVersionStore}
}

// SetRuntimeClient sets the runtime client for reading profile ConfigMaps
// This should be called after creation if a different client is needed for the runtime cluster
func (r *ResourceSubroutine) SetRuntimeClient(clientRuntime ctrlruntimeclient.Client) {
	r.clientRuntime = clientRuntime
}

func (r *ResourceSubroutine) GetName() string {
	return "ResourceSubroutine"
}

func (r *ResourceSubroutine) Finalize(_ context.Context, _ ctrlruntimeclient.Object) (subroutineslib.Result, error) {
	return subroutineslib.OK(), nil
}

func (r *ResourceSubroutine) Finalizers(_ ctrlruntimeclient.Object) []string { // coverage-ignore
	return []string{}
}

func getAnnotations(obj *unstructured.Unstructured) map[string]string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	return annotations
}

// getMetadataValue retrieves a value from annotations first, then falls back to labels for backwards compatibility
func getMetadataValue(obj *unstructured.Unstructured, key string) string {
	annotations := getAnnotations(obj)
	if value, ok := annotations[key]; ok && value != "" {
		return value
	}

	labels := obj.GetLabels()
	if labels != nil {
		return labels[key]
	}

	return ""
}

func (r *ResourceSubroutine) Process(ctx context.Context, runtimeObj ctrlruntimeclient.Object) (res subroutineslib.Result, err error) {
	start := time.Now()
	defer func() {
		labelResult := "success"
		if err != nil {
			labelResult = "error"
		}
		metrics.SubroutineTotal.WithLabelValues(r.GetName(), labelResult).Inc()
		metrics.SubroutineDuration.WithLabelValues(r.GetName()).Observe(time.Since(start).Seconds())
	}()
	inst := runtimeObj.(*unstructured.Unstructured)
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("name", r.GetName())

	deploymentTech, err := r.getDeploymentTechnologyFromProfile(ctx, inst.GetNamespace(), log)
	if err != nil {
		log.Error().Err(err).Str("namespace", inst.GetNamespace()).Msg("Failed to determine deploymentTechnology from profile")
		return subroutineslib.OK(), err
	}
	if deploymentTech == "" {
		log.Warn().Str("namespace", inst.GetNamespace()).Msg("deploymentTechnology not configured in profile, defaulting to fluxcd")
		deploymentTech = "fluxcd"
	}
	log.Debug().Str("deploymentTechnology", deploymentTech).Str("resource", inst.GetName()).Str("namespace", inst.GetNamespace()).Msg("Checking deployment technology for Resource")

	repo := getMetadataValue(inst, "repo")
	artifact := getMetadataValue(inst, "artifact")

	if deploymentTech == "argocd" {
		// argocd logic
		switch artifact {
		case "chart":
			log.Debug().Msg("Update ArgoCD Application targetRevision and repoURL for chart artifact")
			result, err := r.updateArgoCDApplication(ctx, inst, log)
			if err != nil {
				return result, err
			}
		case "image":
			log.Debug().Msg("Update ArgoCD Application Helm values (image.tag) for image artifact")
			result, err := r.updateArgoCDApplicationHelmValues(ctx, inst, log)
			if err != nil {
				return result, err
			}
		default:
			log.Warn().Str("artifact", artifact).Msg("ArgoCD is enabled but artifact is not 'chart' or 'image', skipping")
		}
		return subroutineslib.OK(), nil
	}

	// fluxcd logic
	if repo == "oci" && artifact == "chart" {
		log.Debug().Msg("Create/Update OCI Repo")
		result, err := r.updateOciRepo(ctx, inst, log)
		if err != nil {
			return result, err
		}
		if !result.IsContinue() {
			return result, nil
		}
	}
	if repo == "git" && artifact == "chart" {
		log.Debug().Msg("Create/Update Git Repo")
		result, err := r.updateGitRepo(ctx, inst, log)
		if err != nil {
			return result, err
		}
		if !result.IsContinue() {
			return result, nil
		}
	}
	if repo == "helm" && artifact == "chart" {
		log.Debug().Msg("Create/Update Flux Helm Repository Repo")
		result, err := r.updateHelmRepository(ctx, inst, log)
		if err != nil {
			return result, err
		}
		if !result.IsContinue() {
			return result, nil
		}
		log.Debug().Msg("Update Flux Helm Release Repo")
		result, err = r.updateHelmRelease(ctx, inst, log)
		if err != nil {
			return result, err
		}
		if !result.IsContinue() {
			return result, nil
		}
	}
	if (repo == "helm" && artifact == "image") || (repo == "oci" && artifact == "image") {
		log.Debug().Msg("Update Helm Release with image location")
		result, err := r.updateHelmReleaseImage(ctx, inst, log)
		if err != nil {
			return result, err
		}
		if !result.IsContinue() {
			return result, nil
		}
	}
	return subroutineslib.OK(), nil
}

// updateHelmReleaseImage writes an image Resource's location into the HelmRelease named
// by the "for" annotation: registry, repository and digest are written as siblings of the
// configured path's tag leaf. Without localized coordinates in status.additional the tag
// falls back to the resource version and is written alone.
func (r *ResourceSubroutine) updateHelmReleaseImage(ctx context.Context, inst *unstructured.Unstructured, log *logger.Logger) (subroutineslib.Result, error) {
	defaultNamespace, err := r.getAppNamespaceFromProfile(ctx, inst.GetNamespace(), log)
	if err != nil {
		return subroutineslib.OK(), err
	}
	name, namespace := parseNamespacedName(getMetadataValue(inst, "for"), inst.GetName(), defaultNamespace)
	updatePath := append([]string{"spec", "values"}, parsePath(getMetadataValue(inst, "path"), "image.tag")...)
	versionPath := parsePath(getMetadataValue(inst, "version-path"), "status.resource.version")

	version, found, _ := unstructured.NestedString(inst.Object, versionPath...)
	if !found || version == "" {
		return subroutineslib.OK(), fmt.Errorf("version not available at path %v", versionPath)
	}

	tag := version
	if localizedTag, foundTag, _ := unstructured.NestedString(inst.Object, "status", "additional", "tag"); foundTag && localizedTag != "" {
		tag = localizedTag
	}
	registry, foundReg, _ := unstructured.NestedString(inst.Object, "status", "additional", "registry")
	repository, foundRepo, _ := unstructured.NestedString(inst.Object, "status", "additional", "repository")
	hasCoords := foundReg && registry != "" && foundRepo && repository != ""

	versionLeaf := updatePath[len(updatePath)-1]
	coordLeaves := []string{"registry", "repository", "digest"}
	// A path leaf that is itself a coordinate name holds the tag, so inject nothing.
	leafCollides := slices.Contains(coordLeaves, versionLeaf)
	activeLeaves := make([]string, 0, len(coordLeaves))
	for _, leaf := range coordLeaves {
		if leaf != versionLeaf {
			activeLeaves = append(activeLeaves, leaf)
		}
	}

	coords := map[string]string{}
	if hasCoords && leafCollides {
		log.Warn().Str("leaf", versionLeaf).Msg("Configured path leaf collides with an image coordinate; skipping coordinate injection")
	} else if hasCoords {
		if getMetadataValue(inst, "image-ref") == "combined" {
			// Charts accepting only a single host-qualified repository (openfga's
			// values.schema.json rejects registry/digest outright).
			coords["repository"] = registry + "/" + repository
		} else {
			coords["registry"] = registry
			coords["repository"] = repository
			if digest, foundDigest, _ := unstructured.NestedString(inst.Object, "status", "additional", "digest"); foundDigest && digest != "" {
				coords["digest"] = digest
			}
		}
	}

	// GET the existing HelmRelease so we can do a merge patch instead of SSA.
	// SSA with ForceOwnership would require the full valid spec (chart/chartRef, interval) in the patch,
	// but we only want to update a nested values field without replacing the whole object.
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(helmReleaseGvk)
	if err := r.client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: namespace}, existing); err != nil {
		return subroutineslib.OK(), fmt.Errorf("HelmRelease %s/%s not found: %w", namespace, name, err)
	}

	if err := unstructured.SetNestedField(existing.Object, tag, updatePath...); err != nil {
		return subroutineslib.OK(), err
	}

	// Clear before writing: a digest from an earlier reconcile takes precedence over the
	// tag, so leaving it would deploy the old image despite a tag change.
	parentPath := updatePath[:len(updatePath)-1]
	for _, leaf := range activeLeaves {
		path := appendPath(parentPath, leaf)
		unstructured.RemoveNestedField(existing.Object, path...)
		if value, ok := coords[leaf]; ok {
			if err := unstructured.SetNestedField(existing.Object, value, path...); err != nil {
				return subroutineslib.OK(), err
			}
		}
	}

	if getMetadataValue(inst, "unsuspend") == "true" {
		_ = unstructured.SetNestedField(existing.Object, false, "spec", "suspend")
	}

	if err := r.client.Update(ctx, existing); err != nil {
		log.Error().Err(err).Msg("Failed to update HelmRelease")
		return subroutineslib.OK(), err
	}

	valuesPath := updatePath[2:]
	parentValuesPath := valuesPath[:len(valuesPath)-1]
	r.storeImageVersion(namespace, name, strings.Join(valuesPath, "."), tag)
	for _, leaf := range activeLeaves {
		storePath := strings.Join(appendPath(parentValuesPath, leaf), ".")
		if value, ok := coords[leaf]; ok {
			r.storeImageVersion(namespace, name, storePath, value)
		} else {
			r.removeImageVersion(namespace, name, storePath)
		}
	}
	if getMetadataValue(inst, "unsuspend") == "true" {
		r.storeUnsuspended(namespace, name)
		log.Info().
			Str("helmRelease", name).
			Str("namespace", namespace).
			Str("resource", inst.GetName()).
			Str("version", version).
			Msg("Image injection complete - HelmRelease unsuspended and ready for reconciliation")
	}
	return subroutineslib.OK(), nil
}

func (r *ResourceSubroutine) updateArgoCDApplication(ctx context.Context, inst *unstructured.Unstructured, log *logger.Logger) (subroutineslib.Result, error) {
	repoURL, targetRevision, chartType, err := r.resolveArgoCDSource(inst)
	if err != nil {
		log.Info().Err(err).Msg("Failed to resolve ArgoCD source")
		return subroutineslib.OK(), err
	}
	log.Debug().Str("repoURL", repoURL).Str("targetRevision", targetRevision).Str("type", chartType).Msg("Resolved ArgoCD source")

	appNamespace, err := r.getAppNamespaceFromProfile(ctx, inst.GetNamespace(), log)
	if err != nil {
		return subroutineslib.OK(), err
	}
	appName := trimPMSuffixes(inst.GetName())
	existingApp := &unstructured.Unstructured{}
	existingApp.SetGroupVersionKind(argocdApplicationGvk)
	if err := r.client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: appName, Namespace: appNamespace}, existingApp); err != nil {
		log.Info().Err(err).Str("namespace", appNamespace).Msg("Application not found, waiting for DeploymentSubroutine to create it")
		return subroutineslib.OK(), fmt.Errorf("application %s/%s not found", appNamespace, appName)
	}

	currentRevision, _, _ := unstructured.NestedString(existingApp.Object, "spec", "source", "targetRevision")
	currentRepoURL, _, _ := unstructured.NestedString(existingApp.Object, "spec", "source", "repoURL")
	if currentRevision == targetRevision && currentRepoURL == repoURL {
		return subroutineslib.OK(), nil
	}

	patchObj := &unstructured.Unstructured{}
	patchObj.SetGroupVersionKind(argocdApplicationGvk)
	patchObj.SetName(appName)
	patchObj.SetNamespace(appNamespace)
	if err := unstructured.SetNestedField(patchObj.Object, targetRevision, "spec", "source", "targetRevision"); err != nil {
		return subroutineslib.OK(), err
	}
	if err := unstructured.SetNestedField(patchObj.Object, repoURL, "spec", "source", "repoURL"); err != nil {
		return subroutineslib.OK(), err
	}

	fieldManager := fmt.Sprintf("%s-%s", resourceFieldManager, inst.GetName())
	if err := r.client.Patch(ctx, patchObj, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(fieldManager), ctrlruntimeclient.ForceOwnership); err != nil { //nolint:staticcheck // Apply via Patch is required for unstructured objects
		log.Error().Err(err).Msg("Failed to update ArgoCD Application")
		return subroutineslib.OK(), err
	}

	log.Info().
		Str("application", appName).
		Str("repoURL", repoURL).
		Str("targetRevision", targetRevision).
		Str("previousRevision", currentRevision).
		Msg("Updated ArgoCD Application")
	return subroutineslib.OK(), nil
}

func (r *ResourceSubroutine) resolveArgoCDSource(inst *unstructured.Unstructured) (repoURL, targetRevision, chartType string, err error) {
	getString := func(path ...string) string {
		val, _, _ := unstructured.NestedString(inst.Object, path...)
		return val
	}

	// Helm repository
	if helmRepo := getString("status", "resource", "access", "helmRepository"); helmRepo != "" {
		version := getString("status", "resource", "version")
		if version == "" {
			return "", "", "", fmt.Errorf("version not found for helm chart")
		}
		return helmRepo, version, "helm", nil
	}

	// Git repository
	if gitRepo := getString("status", "resource", "access", "repoUrl"); gitRepo != "" {
		revision := firstNonEmpty(
			getString("status", "resource", "access", "ref"),
			getString("status", "component", "version"),
			getString("status", "resource", "version"),
			getString("status", "resource", "access", "commit"),
		)
		if revision == "" {
			return "", "", "", fmt.Errorf("no ref, version, or commit found for git chart")
		}
		return gitRepo, revision, "git", nil
	}

	// OCI image reference
	imageRef := getString("status", "resource", "access", "imageReference")
	if imageRef == "" {
		imageRef = getString("status", "resource", "imageReference")
	}
	if imageRef == "" {
		return "", "", "", fmt.Errorf("no helmRepository, repoUrl, or imageReference found")
	}

	repoURL, err = ociref.ExtractRepoURL(imageRef)
	if err != nil {
		return "", "", "", err
	}

	version := getString("status", "resource", "version")
	if version == "" {
		return "", "", "", fmt.Errorf("version not found for OCI chart")
	}
	return repoURL, version, "oci", nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (r *ResourceSubroutine) updateArgoCDApplicationHelmValues(ctx context.Context, inst *unstructured.Unstructured, log *logger.Logger) (subroutineslib.Result, error) {
	defaultNamespace, err := r.getAppNamespaceFromProfile(ctx, inst.GetNamespace(), log)
	if err != nil {
		return subroutineslib.OK(), err
	}
	appName, appNamespace := parseNamespacedName(getMetadataValue(inst, "for"), inst.GetName(), defaultNamespace)
	updatePath := parsePath(getMetadataValue(inst, "path"), "image.tag")
	pathStr := strings.Join(updatePath, ".")

	version, found, _ := unstructured.NestedString(inst.Object, "status", "resource", "version")
	if !found {
		return subroutineslib.OK(), fmt.Errorf("version not found")
	}

	existingApp := &unstructured.Unstructured{}
	existingApp.SetGroupVersionKind(argocdApplicationGvk)
	if err := r.client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: appName, Namespace: appNamespace}, existingApp); err != nil {
		log.Info().Err(err).Str("application", appName).Msg("Application not found, waiting for creation")
		return subroutineslib.OK(), fmt.Errorf("application %s/%s not found", appNamespace, appName)
	}

	existingValues, _, _ := unstructured.NestedString(existingApp.Object, "spec", "source", "helm", "values")
	currentTag := getValueFromYAML(existingValues, updatePath)
	if currentTag == version {
		r.storeImageVersion(appNamespace, appName, pathStr, version)
		return subroutineslib.OK(), nil
	}

	updatedValues, err := subroutines.SetHelmValues(existingValues, []subroutines.ImageVersion{{Path: pathStr, Version: version}})
	if err != nil {
		return subroutineslib.OK(), err
	}

	patchObj := &unstructured.Unstructured{}
	patchObj.SetGroupVersionKind(argocdApplicationGvk)
	patchObj.SetName(appName)
	patchObj.SetNamespace(appNamespace)
	if err := unstructured.SetNestedField(patchObj.Object, updatedValues, "spec", "source", "helm", "values"); err != nil {
		return subroutineslib.OK(), err
	}

	fieldManager := fmt.Sprintf("%s-%s", resourceFieldManager, inst.GetName())
	if err := r.client.Patch(ctx, patchObj, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(fieldManager), ctrlruntimeclient.ForceOwnership); err != nil { //nolint:staticcheck // Apply via Patch is required for unstructured objects
		return subroutineslib.OK(), err
	}

	log.Info().Str("application", appName).Str("path", pathStr).Str("version", version).Msg("Updated ArgoCD Application helm values")
	r.storeImageVersion(appNamespace, appName, pathStr, version)
	return subroutineslib.OK(), nil
}

func (r *ResourceSubroutine) storeImageVersion(namespace, name, path, version string) {
	if r.imageVersionStore != nil {
		r.imageVersionStore.Set(namespace, name, path, version)
	}
}

func (r *ResourceSubroutine) storeUnsuspended(namespace, name string) {
	if r.imageVersionStore != nil {
		r.imageVersionStore.SetUnsuspended(namespace, name)
	}
}

func (r *ResourceSubroutine) removeImageVersion(namespace, name, path string) {
	if r.imageVersionStore != nil {
		r.imageVersionStore.Remove(namespace, name, path)
	}
}

// appendPath appends leaf to a copy of parent, so sibling paths never alias its array.
func appendPath(parent []string, leaf string) []string {
	return append(append([]string{}, parent...), leaf)
}

func parseNamespacedName(forVal, defaultName, defaultNamespace string) (name, namespace string) {
	if forVal == "" {
		return trimPMSuffixes(defaultName), defaultNamespace
	}
	if parts := strings.Split(forVal, "/"); len(parts) == 2 {
		return parts[1], parts[0]
	}
	return forVal, defaultNamespace
}

func trimPMSuffixes(name string) string {
	if strings.HasSuffix(name, "-image") {
		return strings.TrimSuffix(name, "-image")
	}
	if strings.HasSuffix(name, "-chart") {
		return strings.TrimSuffix(name, "-chart")
	}
	return name
}

func parsePath(pathStr, defaultPath string) []string {
	if pathStr == "" {
		return strings.Split(defaultPath, ".")
	}
	return strings.Split(pathStr, ".")
}

func getValueFromYAML(yamlStr string, path []string) string {
	if yamlStr == "" {
		return ""
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(yamlStr), &m); err != nil {
		return ""
	}
	val, _ := getNestedString(m, path...)
	return val
}

// getNestedString retrieves a nested string value from a map using a path
func getNestedString(m map[string]any, path ...string) (string, bool) {
	if len(path) == 0 {
		return "", false
	}
	current := m
	for _, key := range path[:len(path)-1] {
		val, ok := current[key]
		if !ok {
			return "", false
		}
		if valMap, ok := val.(map[string]any); ok {
			current = valMap
		} else {
			return "", false
		}
	}
	lastKey := path[len(path)-1]
	val, ok := current[lastKey]
	if !ok {
		return "", false
	}
	if strVal, ok := val.(string); ok {
		return strVal, true
	}
	return "", false
}

func (r *ResourceSubroutine) updateHelmRelease(ctx context.Context, inst *unstructured.Unstructured, log *logger.Logger) (subroutineslib.Result, error) {
	version, found, _ := unstructured.NestedString(inst.Object, "status", "resource", "version")
	if !found || version == "" {
		return subroutineslib.OK(), fmt.Errorf("version not available")
	}

	name := trimPMSuffixes(inst.GetName())
	namespace, err := r.getAppNamespaceFromProfile(ctx, inst.GetNamespace(), log)
	if err != nil {
		return subroutineslib.OK(), err
	}

	// GET the existing HelmRelease so we can do a merge update instead of SSA,
	// which would require a full valid spec (chart.spec.chart, chart.spec.sourceRef, etc.).
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(helmReleaseGvk)
	if err := r.client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: namespace}, existing); err != nil {
		return subroutineslib.OK(), fmt.Errorf("HelmRelease %s/%s not found: %w", namespace, name, err)
	}

	if err := unstructured.SetNestedField(existing.Object, version, "spec", "chart", "spec", "version"); err != nil {
		return subroutineslib.OK(), err
	}

	if getMetadataValue(inst, "unsuspend") == "true" {
		_ = unstructured.SetNestedField(existing.Object, false, "spec", "suspend")
		r.storeUnsuspended(namespace, name)
	}

	if err := r.client.Update(ctx, existing); err != nil {
		log.Error().Err(err).Msg("Failed to update HelmRelease")
		return subroutineslib.OK(), err
	}
	return subroutineslib.OK(), nil
}

func (r *ResourceSubroutine) updateHelmRepository(ctx context.Context, inst *unstructured.Unstructured, log *logger.Logger) (subroutineslib.Result, error) {
	url, found, _ := unstructured.NestedString(inst.Object, "status", "resource", "access", "helmRepository")
	if !found || url == "" {
		return subroutineslib.StopWithRequeue(requeueShort, "helmRepository not available in Resource status"), nil
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(helmRepoGvk)
	obj.SetName(trimPMSuffixes(inst.GetName()))
	ns, err := r.getAppNamespaceFromProfile(ctx, inst.GetNamespace(), log)
	if err != nil {
		return subroutineslib.OK(), err
	}
	obj.SetNamespace(ns)
	_ = unstructured.SetNestedField(obj.Object, url, "spec", "url")
	_ = unstructured.SetNestedField(obj.Object, "generic", "spec", "provider")
	_ = unstructured.SetNestedField(obj.Object, "5m", "spec", "interval")

	if err := r.client.Patch(ctx, obj, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(resourceFieldManager), ctrlruntimeclient.ForceOwnership); err != nil { //nolint:staticcheck // Apply via Patch is required for unstructured objects
		log.Error().Err(err).Msg("Failed to apply HelmRepository")
		return subroutineslib.OK(), err
	}
	return subroutineslib.OK(), nil
}

func (r *ResourceSubroutine) updateOciRepo(ctx context.Context, inst *unstructured.Unstructured, log *logger.Logger) (subroutineslib.Result, error) {
	version, found, err := unstructured.NestedString(inst.Object, "status", "resource", "version")
	if err != nil || !found || version == "" {
		log.Info().Err(err).Msg("Failed to get version from Resource status")
		if err == nil {
			err = fmt.Errorf("version not available in Resource status")
		}
		return subroutineslib.OK(), err
	}
	url, found, err := unstructured.NestedString(inst.Object, "status", "resource", "access", "imageReference")
	if err != nil || !found || url == "" {
		log.Info().Err(err).Msg("Failed to get imageReference from Resource status")
		if err == nil {
			err = fmt.Errorf("imageReference not available in Resource status")
		}
		return subroutineslib.OK(), err
	}

	url, err = ociref.NormalizeOCIURL(url, version)
	if err != nil {
		log.Error().Err(err).Str("url", url).Msg("Failed to parse Resource url")
		return subroutineslib.OK(), err
	}

	// Update or create oci repo
	log.Info().Msg("Processing OCI Chart Resource")
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(ociRepoGvk)
	obj.SetName(trimPMSuffixes(inst.GetName()))
	ns, err := r.getAppNamespaceFromProfile(ctx, inst.GetNamespace(), log)
	if err != nil {
		return subroutineslib.OK(), err
	}
	obj.SetNamespace(ns)

	// Set desired fields
	if err := unstructured.SetNestedField(obj.Object, version, "spec", "ref", "tag"); err != nil {
		return subroutineslib.OK(), err
	}
	if err := unstructured.SetNestedField(obj.Object, url, "spec", "url"); err != nil {
		return subroutineslib.OK(), err
	}
	if err := unstructured.SetNestedField(obj.Object, "generic", "spec", "provider"); err != nil {
		return subroutineslib.OK(), err
	}
	if err := unstructured.SetNestedField(obj.Object, "1m0s", "spec", "interval"); err != nil {
		return subroutineslib.OK(), err
	}
	if err := unstructured.SetNestedMap(obj.Object, map[string]any{
		"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
		"operation": "copy",
	}, "spec", "layerSelector"); err != nil {
		return subroutineslib.OK(), err
	}

	// Apply using SSA (creates if not exists, updates if exists)
	if err := r.client.Patch(ctx, obj, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(resourceFieldManager), ctrlruntimeclient.ForceOwnership); err != nil { //nolint:staticcheck // Apply via Patch is required for unstructured objects
		log.Error().Err(err).Msg("Failed to apply OCIRepository")
		return subroutineslib.OK(), err
	}
	return subroutineslib.OK(), nil
}

func (r *ResourceSubroutine) updateGitRepo(ctx context.Context, inst *unstructured.Unstructured, log *logger.Logger) (subroutineslib.Result, error) {
	commit, found, err := unstructured.NestedString(inst.Object, "status", "resource", "access", "commit")
	if err != nil || !found || commit == "" {
		log.Info().Err(err).Msg("Failed to get commit from Resource status")
		if err == nil {
			err = fmt.Errorf("commit not available in Resource status")
		}
		return subroutineslib.OK(), err
	}

	url, found, err := unstructured.NestedString(inst.Object, "status", "resource", "access", "repoUrl")
	if err != nil || !found || url == "" {
		log.Info().Err(err).Msg("Failed to get repoUrl from Resource status")
		return subroutineslib.OK(), fmt.Errorf("repoUrl not available in Resource status")
	}

	// Update or create git repo
	log.Info().Msg("Processing Git Repository Resource")
	obj := &unstructured.Unstructured{}

	obj.SetGroupVersionKind(gitRepoGvk)
	obj.SetName(trimPMSuffixes(inst.GetName()))
	ns, err := r.getAppNamespaceFromProfile(ctx, inst.GetNamespace(), log)
	if err != nil {
		return subroutineslib.OK(), err
	}
	obj.SetNamespace(ns)

	// Set desired fields
	if err := unstructured.SetNestedField(obj.Object, commit, "spec", "ref", "commit"); err != nil {
		return subroutineslib.OK(), err
	}
	if err := unstructured.SetNestedField(obj.Object, url, "spec", "url"); err != nil {
		return subroutineslib.OK(), err
	}
	if err := unstructured.SetNestedField(obj.Object, "1m0s", "spec", "interval"); err != nil {
		return subroutineslib.OK(), err
	}
	if err := unstructured.SetNestedField(obj.Object, "5m", "spec", "timeout"); err != nil {
		return subroutineslib.OK(), err
	}

	// Apply using SSA (creates if not exists, updates if exists)
	if err := r.client.Patch(ctx, obj, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(resourceFieldManager), ctrlruntimeclient.ForceOwnership); err != nil { //nolint:staticcheck // Apply via Patch is required for unstructured objects
		log.Error().Err(err).Msg("Failed to apply GitRepository")
		return subroutineslib.OK(), err
	}
	return subroutineslib.OK(), nil
}

// getAppNamespaceFromProfile returns the namespace where Application CRs are deployed.
// If the profile sets a deploymentNamespace (infra or components section), use that.
// Otherwise fall back to the Resource CR's own namespace.
func (r *ResourceSubroutine) getAppNamespaceFromProfile(ctx context.Context, resourceNamespace string, log *logger.Logger) (string, error) {
	configMapNames := profileConfigMapNames

	for _, cmName := range configMapNames {
		configMap := &corev1.ConfigMap{}
		if err := r.clientRuntime.Get(ctx, types.NamespacedName{Name: cmName, Namespace: resourceNamespace}, configMap); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", fmt.Errorf("failed to read profile ConfigMap %s/%s: %w", resourceNamespace, cmName, err)
		}

		profileYAML, ok := configMap.Data[profileConfigMapKey]
		if !ok {
			continue
		}

		var profile map[string]any
		if err := yaml.Unmarshal([]byte(profileYAML), &profile); err != nil {
			continue
		}

		if infra, ok := profile["infra"].(map[string]any); ok {
			if ns, ok := infra["deploymentNamespace"].(string); ok && ns != "" {
				log.Debug().Str("appNamespace", ns).Str("source", "infra.deploymentNamespace").Msg("Resolved app namespace from profile")
				return ns, nil
			}
		}

		if components, ok := profile["components"].(map[string]any); ok {
			if ns, ok := components["deploymentNamespace"].(string); ok && ns != "" {
				log.Debug().Str("appNamespace", ns).Str("source", "components.deploymentNamespace").Msg("Resolved app namespace from profile")
				return ns, nil
			}
		}
	}

	return resourceNamespace, nil
}

func (r *ResourceSubroutine) getDeploymentTechnologyFromProfile(ctx context.Context, namespace string, log *logger.Logger) (string, error) {
	platformMeshList := &pmcorev1alpha1.PlatformMeshList{}
	if err := r.clientRuntime.List(ctx, platformMeshList, ctrlruntimeclient.InNamespace(namespace)); err != nil {
		log.Warn().Err(err).Str("namespace", namespace).Msg("Failed to list PlatformMesh instances, trying direct ConfigMap lookup")
		return r.getDeploymentTechnologyFromConfigMapDirect(ctx, namespace, log)
	}

	if len(platformMeshList.Items) == 0 {
		log.Debug().Str("namespace", namespace).Msg("No PlatformMesh instances found in namespace, defaulting deploymentTechnology")
		return "", nil
	}

	if len(platformMeshList.Items) > 1 {
		names := make([]string, len(platformMeshList.Items))
		for i, item := range platformMeshList.Items {
			names[i] = item.Name
		}
		log.Warn().Strs("platformMeshInstances", names).Str("namespace", namespace).Msg("Multiple PlatformMesh instances found in namespace, using first one")
	}

	pm := platformMeshList.Items[0]
	log.Info().Str("namespace", namespace).Str("platformMesh", pm.Name).Msg("Found PlatformMesh instance, reading deploymentTechnology from profile")

	deploymentTech, err := subroutines.GetDeploymentTechnologyFromProfile(ctx, r.clientRuntime, &pm)
	if err != nil {
		log.Error().Err(err).Str("platformMesh", pm.Name).Msg("Failed to read deploymentTechnology from profile")
		return "", err
	}
	log.Info().Str("deploymentTechnology", deploymentTech).Str("platformMesh", pm.Name).Str("configMap", pm.Name+"-profile").Msg("Read deploymentTechnology from profile")
	return deploymentTech, nil
}

func (r *ResourceSubroutine) getDeploymentTechnologyFromConfigMapDirect(ctx context.Context, namespace string, log *logger.Logger) (string, error) {
	configMapNames := profileConfigMapNames

	for _, cmName := range configMapNames {
		configMap := &corev1.ConfigMap{}
		if err := r.clientRuntime.Get(ctx, types.NamespacedName{Name: cmName, Namespace: namespace}, configMap); err != nil {
			if apierrors.IsNotFound(err) {
				log.Debug().Str("configMap", cmName).Str("namespace", namespace).Msg("Profile ConfigMap not found, trying next")
				continue
			}
			return "", fmt.Errorf("failed to read profile ConfigMap %s/%s: %w", namespace, cmName, err)
		}

		log.Info().Str("configMap", cmName).Str("namespace", namespace).Msg("Found ConfigMap, reading profile")

		profileYAML, ok := configMap.Data[profileConfigMapKey]
		if !ok {
			log.Warn().Str("configMap", cmName).Msg("ConfigMap found but profile key missing")
			continue
		}

		var profile map[string]any
		if err := yaml.Unmarshal([]byte(profileYAML), &profile); err != nil {
			return "", fmt.Errorf("failed to parse profile YAML from ConfigMap %s/%s: %w", namespace, cmName, err)
		}

		// Check infra section first
		if infra, ok := profile["infra"].(map[string]any); ok {
			if dt, ok := infra["deploymentTechnology"].(string); ok && dt != "" {
				log.Info().Str("deploymentTechnology", strings.ToLower(dt)).Str("configMap", cmName).Str("section", "infra").Msg("Read deploymentTechnology from profile ConfigMap (direct lookup)")
				return strings.ToLower(dt), nil
			}
		}

		// Check components section
		if components, ok := profile["components"].(map[string]any); ok {
			if dt, ok := components["deploymentTechnology"].(string); ok && dt != "" {
				log.Info().Str("deploymentTechnology", strings.ToLower(dt)).Str("configMap", cmName).Str("section", "components").Msg("Read deploymentTechnology from profile ConfigMap (direct lookup)")
				return strings.ToLower(dt), nil
			}
		}

		log.Debug().Str("configMap", cmName).Msg("Profile ConfigMap found but deploymentTechnology not configured")
		return "", nil
	}

	log.Warn().Str("namespace", namespace).Msgf("no profile ConfigMap found in namespace (tried: %v), defaulting deploymentTechnology", configMapNames)
	return "", nil
}
