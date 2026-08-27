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
	stderrors "errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"text/template"

	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

var argoApplicationGVK = schema.GroupVersionKind{
	Group:   "argoproj.io",
	Version: "v1alpha1",
	Kind:    "Application",
}

// errSkipObject is a sentinel returned by postProcessObj to signal that the object
// should be silently skipped (not applied to the cluster) without aborting the loop.
var errSkipObject = stderrors.New("skip object")

const (
	// Field manager names for Server-Side Apply
	fieldManagerDeployment = "platform-mesh-deployment"
)

// updateObjectMetadata updates labels and annotations from desired to existing.
func updateObjectMetadata(existing, desired *unstructured.Unstructured) {
	if labels := desired.GetLabels(); labels != nil {
		existing.SetLabels(labels)
	}
	if annotations := desired.GetAnnotations(); annotations != nil {
		existing.SetAnnotations(annotations)
	}
}

// renderAndApplyTemplates renders and applies all YAML templates in a directory.
// skipFile, if non-nil, is called for each file; returning true skips that file.
// postProcessObj, if non-nil, is called on each rendered object before applying.
func (r *DeploymentSubroutine) renderAndApplyTemplates(
	ctx context.Context,
	dir string,
	tmplVars map[string]any,
	k8sClient ctrlruntimeclient.Client,
	log *logger.Logger,
	templateType string,
	skipFile func(fileName string) bool,
	postProcessObj func(ctx context.Context, obj *unstructured.Unstructured) error,
) error {
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(d.Name(), ".yaml") {
			return nil
		}

		if skipFile != nil && skipFile(d.Name()) {
			return nil
		}

		// Read and render template (supports multi-document YAML)
		objs, err := r.renderTemplateFile(path, tmplVars, log)
		if err != nil {
			return errors.Wrap(err, "Failed to render template: %s", path)
		}

		for _, obj := range objs {
			if postProcessObj != nil {
				if err := postProcessObj(ctx, obj); err != nil {
					if stderrors.Is(err, errSkipObject) {
						continue
					}
					return errors.Wrap(err, "Failed to post-process rendered object from template: %s (%s/%s)", path, obj.GetKind(), obj.GetName())
				}
			}

			// Apply the rendered manifest
			if err := k8sClient.Patch(ctx, obj, ctrlruntimeclient.Apply, ctrlruntimeclient.FieldOwner(fieldManagerDeployment), ctrlruntimeclient.ForceOwnership); err != nil { //nolint:staticcheck // Apply via Patch is required for unstructured objects
				return errors.Wrap(err, "Failed to apply rendered manifest from template: %s (%s/%s)", path, obj.GetKind(), obj.GetName())
			}
		}

		return nil
	})

	if err != nil {
		log.Error().Err(err).Str("type", templateType).Msg("Failed to render and apply templates")
		return err
	}

	return nil
}

// renderAndApplyTemplatesWithRouter is like renderAndApplyTemplates but instead of applying to a
// single client it delegates the Apply to applyFunc, which can route each object to a different client.
func (r *DeploymentSubroutine) renderAndApplyTemplatesWithRouter(
	ctx context.Context,
	dir string,
	tmplVars map[string]any,
	log *logger.Logger,
	templateType string,
	skipFile func(fileName string) bool,
	applyFunc func(ctx context.Context, obj *unstructured.Unstructured) error,
) error {
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(d.Name(), ".yaml") {
			return nil
		}

		if skipFile != nil && skipFile(d.Name()) {
			return nil
		}

		objs, err := r.renderTemplateFile(path, tmplVars, log)
		if err != nil {
			return errors.Wrap(err, "Failed to render template: %s", path)
		}

		for _, obj := range objs {
			if err := applyFunc(ctx, obj); err != nil {
				return errors.Wrap(err, "Failed to apply rendered manifest from template: %s (%s/%s)", path, obj.GetKind(), obj.GetName())
			}
		}

		return nil
	})

	if err != nil {
		log.Error().Err(err).Str("type", templateType).Msg("Failed to render and apply templates")
		return err
	}

	return nil
}

// renderTemplateFile reads a template file, renders it, and returns all unstructured objects.
// Supports multi-document YAML (documents separated by "---").
// Returns an empty slice if the template renders empty.
func (r *DeploymentSubroutine) renderTemplateFile(path string, tmplVars map[string]any, log *logger.Logger) ([]*unstructured.Unstructured, error) {
	templateBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to read template file")
	}

	tmpl, err := template.New(filepath.Base(path)).Funcs(templateFuncMap()).Parse(string(templateBytes))
	if err != nil {
		return nil, errors.Wrap(err, "Failed to parse template")
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, tmplVars); err != nil {
		return nil, errors.Wrap(err, "Failed to execute template")
	}

	renderedStr := strings.TrimSpace(rendered.String())
	if renderedStr == "" {
		log.Debug().Str("path", path).Msg("Template rendered empty, skipping")
		return nil, nil
	}

	// Split multi-document YAML. Handle both "---\n" at the start and "\n---\n" between documents.
	// Strip a leading document separator if present.
	renderedStr = strings.TrimPrefix(renderedStr, "---\n")
	renderedStr = strings.TrimPrefix(renderedStr, "---\r\n")
	docs := strings.Split(renderedStr, "\n---\n")
	var objs []*unstructured.Unstructured
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		var objMap map[string]any
		if err := yaml.Unmarshal([]byte(doc), &objMap); err != nil {
			return nil, errors.Wrap(err, "Failed to unmarshal rendered YAML from template %s (size: %d bytes)", path, len(doc))
		}

		// Some templates will, once rendered, leave behind only their boilerplate comment,
		// which will not trigger the emptiness check above.
		if objMap != nil {
			objs = append(objs, &unstructured.Unstructured{Object: objMap})
		}
	}
	return objs, nil
}

// helmReleaseKey identifies a HelmRelease the way Flux resolves a spec.dependsOn entry.
func helmReleaseKey(namespace, name string) string {
	return namespace + "/" + name
}

// dependencyKey returns the HelmRelease a dependsOn entry points at, or false if the entry
// has no usable name.
func dependencyKey(entry any, defaultNamespace string) (string, bool) {
	dep, ok := entry.(map[string]any)
	if !ok {
		return "", false
	}
	name, ok := dep["name"].(string)
	if !ok || name == "" {
		return "", false
	}
	namespace, ok := dep["namespace"].(string)
	if !ok || namespace == "" {
		// Flux resolves a namespace-less dependency in the HelmRelease's own namespace.
		namespace = defaultNamespace
	}
	return helmReleaseKey(namespace, name), true
}

// infraHelmReleaseNamespace resolves where the infra HelmReleases are rendered:
// deploymentNamespace, then helmReleaseNamespace, then the fallback. The infra render and the
// dependency pruning must agree here, so both call this with the infra profile merged with
// templateVars and inst.Namespace as fallback. Anything else and the pruning stops matching.
func infraHelmReleaseNamespace(infraVars map[string]any, fallback string) string {
	if deployNs, ok := infraVars["deploymentNamespace"].(string); ok && deployNs != "" {
		return deployNs
	}
	if hrNs, ok := infraVars["helmReleaseNamespace"].(string); ok && hrNs != "" {
		return hrNs
	}
	return fallback
}

// disabledHelmReleases collects the HelmReleases we deliberately do not render, keyed as
// Flux resolves dependsOn. Mirrors the template gates: infra components are gated on
// "<component>.enabled" (missing flag counts as off), services additionally on
// skipHelmRelease.
func disabledHelmReleases(infraProfile, services map[string]any, infraNamespace, componentsNamespace string) map[string]struct{} {
	disabled := make(map[string]struct{})

	for _, component := range infraProfile {
		config, ok := component.(map[string]any)
		if !ok {
			continue
		}
		// Only entries with a release name describe a HelmRelease.
		name, ok := config["name"].(string)
		if !ok || name == "" {
			continue
		}
		if isZeroValue(config["enabled"]) {
			disabled[helmReleaseKey(infraNamespace, name)] = struct{}{}
		}
	}

	for service, serviceConfig := range services {
		config, ok := serviceConfig.(map[string]any)
		if !ok {
			continue
		}
		if isZeroValue(config["enabled"]) || !isZeroValue(config["skipHelmRelease"]) {
			disabled[helmReleaseKey(componentsNamespace, service)] = struct{}{}
		}
	}

	return disabled
}

// pruneDisabledDependencies drops dependsOn entries pointing at a disabled component. Flux
// waits in DependencyNotReady until a dependency exists and is ready, so an entry we never
// render stalls the dependent release forever.
//
// Only provably disabled targets go. An unknown name might be someone else's HelmRelease, and
// dropping a typo silently would hide a real misconfiguration.
func pruneDisabledDependencies(services map[string]any, disabled map[string]struct{}, defaultNamespace string, log *logger.Logger) {
	for service, serviceConfig := range services {
		config, ok := serviceConfig.(map[string]any)
		if !ok {
			continue
		}
		dependsOn, ok := config["dependsOn"].([]any)
		if !ok {
			continue
		}

		config["dependsOn"] = slices.DeleteFunc(slices.Clone(dependsOn), func(entry any) bool {
			key, resolvable := dependencyKey(entry, defaultNamespace)
			if !resolvable {
				return false
			}
			if _, isDisabled := disabled[key]; !isDisabled {
				return false
			}
			log.Warn().
				Str("service", service).
				Str("dependency", key).
				Msg("Dropping dependsOn entry: the dependency is disabled and is never deployed")
			return true
		})
	}
}

// helper: functions for Helm-like templates in components gotemplates
func isZeroValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.Len() == 0
	case reflect.Slice, reflect.Map:
		return rv.Len() == 0
	}
	return rv.IsZero()
}

func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"default": func(d, v any) any {
			if isZeroValue(v) {
				return d
			}
			return v
		},
		"toYaml": func(v any) (string, error) {
			b, err := yaml.Marshal(v)
			return string(b), err
		},
		"nindent": func(spaces int, s string) string {
			if s == "" {
				return ""
			}
			pad := strings.Repeat(" ", spaces)
			lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
			// Filter out empty lines at the start
			startIdx := 0
			for startIdx < len(lines) && strings.TrimSpace(lines[startIdx]) == "" {
				startIdx++
			}
			if startIdx >= len(lines) {
				return ""
			}
			// Filter out empty lines at the end
			endIdx := len(lines)
			for endIdx > startIdx && strings.TrimSpace(lines[endIdx-1]) == "" {
				endIdx--
			}
			// Indent non-empty lines
			for i := startIdx; i < endIdx; i++ {
				if strings.TrimSpace(lines[i]) != "" {
					lines[i] = pad + lines[i]
				}
			}
			result := strings.Join(lines[startIdx:endIdx], "\n")
			if result != "" {
				result = "\n" + result + "\n"
			}
			return result
		},
	}
}

// preserveExistingArgoSourceFields checks if an existing ArgoCD Application has repoURL/targetRevision
// values set by ResourceSubroutine and removes those fields from the new object to preserve them.
// This prevents DeploymentSubroutine from overwriting values managed by ResourceSubroutine.
func (r *DeploymentSubroutine) preserveExistingArgoSourceFields(
	ctx context.Context,
	objMap map[string]any,
	name, namespace string,
	log *logger.Logger,
) {
	existingApp := &unstructured.Unstructured{}
	existingApp.SetGroupVersionKind(argoApplicationGVK)

	if err := r.clientInfra.Get(ctx, ctrlruntimeclient.ObjectKey{Name: name, Namespace: namespace}, existingApp); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Warn().Err(err).Str("app", name).Msg("Failed to get existing ArgoCD Application, skipping field preservation")
		}
		// Application doesn't exist yet (or transient error) — nothing to preserve
		return
	}

	// Application exists - check if repoURL and targetRevision are already set (not placeholders)
	existingRepoURL, found, _ := unstructured.NestedString(existingApp.Object, "spec", "source", "repoURL")
	existingTargetRevision, foundRev, _ := unstructured.NestedString(existingApp.Object, "spec", "source", "targetRevision")

	// Check if the new object has repoURL/targetRevision before trying to preserve
	var newRepoURL, newTargetRevision string
	if spec, ok := objMap["spec"].(map[string]any); ok {
		if source, ok := spec["source"].(map[string]any); ok {
			if url, ok := source["repoURL"].(string); ok {
				newRepoURL = url
			}
			if rev, ok := source["targetRevision"].(string); ok {
				newTargetRevision = rev
			}
		}
	}

	// Remove placeholder values from the patch — they must never be applied to the cluster.
	// ResourceSubroutine is responsible for setting the real values.
	// Also preserve any real value that ResourceSubroutine has already written.
	if spec, ok := objMap["spec"].(map[string]any); ok {
		if source, ok := spec["source"].(map[string]any); ok {
			if newRepoURL == argoPlaceholderRepoURL {
				// Never apply the placeholder — always strip it so ResourceSubroutine owns the field.
				source["repoURL"] = existingRepoURL
				if found && existingRepoURL != "" && existingRepoURL != argoPlaceholderRepoURL {
					log.Debug().Str("app", name).Msg("Preserving existing repoURL from ResourceSubroutine")
				}
			} else if found && existingRepoURL != "" && existingRepoURL != argoPlaceholderRepoURL && existingRepoURL != newRepoURL {
				// Preserve a real value already set by ResourceSubroutine even when the template
				// provides a different (non-placeholder) value.
				source["repoURL"] = existingRepoURL
				log.Debug().Str("app", name).Msg("Preserving existing repoURL from ResourceSubroutine")
			}

			if newTargetRevision == argoPlaceholderRepoURL {
				// Never apply the placeholder for targetRevision either.
				delete(source, "targetRevision")
				if foundRev && existingTargetRevision != "" && existingTargetRevision != argoPlaceholderRepoURL {
					log.Debug().Str("app", name).Msg("Preserving existing targetRevision from ResourceSubroutine")
				}
			} else if foundRev && existingTargetRevision != "" && existingTargetRevision != argoPlaceholderRepoURL && existingTargetRevision != newTargetRevision {
				delete(source, "targetRevision")
				log.Debug().Str("app", name).Msg("Preserving existing targetRevision from ResourceSubroutine")
			}
		}
	}
}

// deploymentTechFileFilter returns a function that skips template files not matching the active deployment technology.
// For argocd: skips helmrelease and kustomization files.
// For fluxcd: skips application files.
func deploymentTechFileFilter(deploymentTech string, log *logger.Logger) func(fileName string) bool {
	return func(fileName string) bool {
		if deploymentTech == deploymentTechArgoCD && (strings.HasPrefix(fileName, "helmrelease") || strings.HasPrefix(fileName, "kustomization")) {
			log.Debug().Str("file", fileName).Str("deploymentTechnology", deploymentTech).Msg("Skipping FluxCD template, ArgoCD is enabled")
			return true
		}
		if deploymentTech == deploymentTechFluxCD && strings.HasPrefix(fileName, "application") {
			log.Debug().Str("file", fileName).Str("deploymentTechnology", deploymentTech).Msg("Skipping ArgoCD template, FluxCD is enabled")
			return true
		}
		return false
	}
}

// infraManifestPostProcess returns a post-process function that adjusts rendered infra manifests
// before they are applied to the cluster. For ArgoCD Applications it preserves source fields set by
// ResourceSubroutine; for FluxCD HelmReleases it merges Resource-managed image versions and respects
// unsuspend state.
func (r *DeploymentSubroutine) infraManifestPostProcess(ctx context.Context, log *logger.Logger) func(ctx context.Context, obj *unstructured.Unstructured) error {
	return func(ctx context.Context, obj *unstructured.Unstructured) error {
		if obj.GetKind() == "Application" && obj.GetAPIVersion() == "argoproj.io/v1alpha1" {
			// preserveExistingArgoSourceFields strips placeholder values from the patch when the
			// Application already exists (so ResourceSubroutine-managed fields are not overwritten),
			// and is a no-op for new Applications (leaving the placeholder in the patch so ArgoCD
			// creates the Application object — ArgoCD accepts placeholder repoURLs and shows them
			// as degraded until ResourceSubroutine sets the real values).
			r.preserveExistingArgoSourceFields(ctx, obj.Object, obj.GetName(), obj.GetNamespace(), log)
			r.mergeImageVersionsIntoHelmValues(obj.Object, obj.GetName(), obj.GetNamespace(), log)
		}
		if obj.GetKind() == "HelmRelease" && obj.GetAPIVersion() == "helm.toolkit.fluxcd.io/v2" {
			r.mergeImageVersionsIntoHelmReleaseValues(obj, obj.GetName(), obj.GetNamespace(), log)

			// Diagnostic logging for suspended HelmReleases waiting on image injection
			suspended, found, _ := unstructured.NestedBool(obj.Object, "spec", "suspend")
			if found && suspended && r.imageVersionStore != nil {
				// Check if this HelmRelease has been unsuspended by ResourceSubroutine
				if !r.imageVersionStore.IsUnsuspended(obj.GetNamespace(), obj.GetName()) {
					// HelmRelease is suspended and has not been unsuspended yet - likely waiting for image injection
					log.Warn().
						Str("helmRelease", obj.GetName()).
						Str("namespace", obj.GetNamespace()).
						Msg("HelmRelease created in suspended state. It will remain suspended until ResourceSubroutine processes OCM imageResources and sets suspend=false. If this HelmRelease never starts, verify that its imageResources configuration includes an entry with 'unsuspend: \"true\"' annotation.")
				}
			}
		}
		return nil
	}
}
