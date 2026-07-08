/*
Copyright 2026.

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

package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	gcerrors "go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/provider-operator/pkg/ocm"
)

const (
	DeploySubroutineName      = "DeploySubroutine"
	deploySubroutineFinalizer = "providers.platform-mesh.io/runtime-deployments"
	deployRequeueDuration     = 10 * time.Second
)

var (
	deployOCIRepoGVK = schema.GroupVersionKind{
		Group:   "source.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    "OCIRepository",
	}
	deployHelmRepoGVK = schema.GroupVersionKind{
		Group:   "source.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    "HelmRepository",
	}
	deployHelmReleaseGVK = schema.GroupVersionKind{
		Group:   "helm.toolkit.fluxcd.io",
		Version: "v2",
		Kind:    "HelmRelease",
	}
	deployOCMRepositoryGVK = schema.GroupVersionKind{
		Group:   "delivery.ocm.software",
		Version: "v1alpha1",
		Kind:    "Repository",
	}
	deployOCMComponentGVK = schema.GroupVersionKind{
		Group:   "delivery.ocm.software",
		Version: "v1alpha1",
		Kind:    "Component",
	}
	deployOCMResourceGVK = schema.GroupVersionKind{
		Group:   "delivery.ocm.software",
		Version: "v1alpha1",
		Kind:    "Resource",
	}
)

const defaultOCMResourceName = "chart"

// ocmDeploymentName resolves the name used for the generated Repository, Component,
// Resource, OCIRepository and HelmRelease: the explicit name, else the resource name
// (when not the default), else the last referencePath element, else the last segment of
// the component name.
func ocmDeploymentName(ocm *providersv1alpha1.OCMComponentSpec) string {
	if ocm.Name != "" {
		return ocm.Name
	}
	if ocm.ResourceName != "" && ocm.ResourceName != defaultOCMResourceName {
		return ocm.ResourceName
	}
	if n := len(ocm.ReferencePath); n > 0 && ocm.ReferencePath[n-1].Name != "" {
		return ocm.ReferencePath[n-1].Name
	}
	return chartResourceName(ocm.Component)
}

// splitRegistry splits an OCM/OCI registry root (e.g. "ghcr.io/platform-mesh") into the
// host (baseUrl) and the remaining sub-path for a delivery.ocm.software Repository.
func splitRegistry(registry string) (baseURL, subPath string) {
	baseURL, subPath, _ = strings.Cut(registry, "/")
	return baseURL, subPath
}

// fluxSourceGVK returns the Flux source object kind for a component's source type.
func fluxSourceGVK(flux *providersv1alpha1.FluxComponentSpec) schema.GroupVersionKind {
	if flux.Type == providersv1alpha1.FluxSourceTypeHelm {
		return deployHelmRepoGVK
	}
	return deployOCIRepoGVK
}

// parseFluxValues unmarshals the component's Helm values, returning nil when none are set.
func parseFluxValues(flux *providersv1alpha1.FluxComponentSpec) (map[string]interface{}, error) {
	if len(flux.Values.Raw) == 0 {
		return nil, nil
	}
	var values map[string]interface{}
	if err := json.Unmarshal(flux.Values.Raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}

// chartResourceName returns the last path segment of a chart's OCI repository path.
// e.g. "github.com/platform-mesh/wildwest-controller" returns "wildwest-controller"
func chartResourceName(chart string) string {
	lastSlashIndex := strings.LastIndexByte(chart, '/') + 1
	return chart[lastSlashIndex:]
}

// DeploySubroutine creates Flux OCIRepository and HelmRelease objects on the
// runtime cluster for the Flux components referenced in spec.runtimeDeployments.
// Subsequent reconciliations detect drift via the HelmRelease Ready condition.
type DeploySubroutine struct {
	client  client.Client
	limiter workqueue.TypedRateLimiter[*providersv1alpha1.ManagedProvider]
}

func NewDeploySubroutine(cl client.Client) (*DeploySubroutine, error) {
	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[*providersv1alpha1.ManagedProvider](
		ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %v", err)
	}
	return &DeploySubroutine{client: cl, limiter: rl}, nil
}

func (r *DeploySubroutine) GetName() string {
	return DeploySubroutineName
}

func (r *DeploySubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	inst := obj.(*providersv1alpha1.ManagedProvider)

	result, err := r.doRuntimeDeployments(ctx, inst)
	if err != nil {
		return subroutines.OK(), err
	}
	if !result.IsContinue() {
		inst.Status.Phase = providersv1alpha1.ManagedProviderPhaseDeploying
		return result, nil
	}

	inst.Status.Phase = providersv1alpha1.ManagedProviderPhaseReady
	return subroutines.OK(), nil
}

func (r *DeploySubroutine) doRuntimeDeployments(ctx context.Context, managedProvider *providersv1alpha1.ManagedProvider) (subroutines.Result, error) {
	runtimeKubeconfigSecretName := managedProvider.Spec.RuntimeKubeconfigSecretName

	for _, component := range managedProvider.Spec.RuntimeDeployments {
		switch {
		case component.Flux != nil:
			flux := component.Flux
			name := chartResourceName(flux.Chart)
			result, err := r.deployFluxComponent(ctx, managedProvider.Namespace, name, flux, runtimeKubeconfigSecretName)
			if err != nil {
				return subroutines.OK(), err
			}
			if !result.IsContinue() {
				return result, nil
			}
		case component.OCM != nil:
			ocm := component.OCM
			name := ocmDeploymentName(ocm)
			result, err := r.deployOCMComponent(ctx, managedProvider.Namespace, name, ocm, runtimeKubeconfigSecretName)
			if err != nil {
				return subroutines.OK(), err
			}
			if !result.IsContinue() {
				return result, nil
			}
		}
	}

	return subroutines.OK(), nil
}

// deployFluxComponent dispatches to the OCI or classic Helm-repository deploy path
// based on the component source type (defaulting to OCI).
func (r *DeploySubroutine) deployFluxComponent(ctx context.Context, namespace, name string, flux *providersv1alpha1.FluxComponentSpec, runtimeKubeconfigSecretName string) (subroutines.Result, error) {
	if flux.Type == providersv1alpha1.FluxSourceTypeHelm {
		return r.deployFluxHelmRepo(ctx, namespace, name, flux, runtimeKubeconfigSecretName)
	}
	return r.deployFluxOCI(ctx, namespace, name, flux, runtimeKubeconfigSecretName)
}

// deployFluxOCI deploys a chart packaged as an OCI artifact via a Flux OCIRepository
// referenced by a HelmRelease through chartRef.
func (r *DeploySubroutine) deployFluxOCI(ctx context.Context, namespace, name string, flux *providersv1alpha1.FluxComponentSpec, runtimeKubeconfigSecretName string) (subroutines.Result, error) {
	ociURL := fmt.Sprintf("oci://%s/%s", flux.Registry, flux.Chart)
	values, err := parseFluxValues(flux)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to unmarshal values for %s", name)
	}
	return r.reconcileResolvedOCIChart(ctx, namespace, name, ociURL, flux.Version, flux.Insecure, values, runtimeKubeconfigSecretName)
}

// reconcileResolvedOCIChart creates/updates a Flux OCIRepository (pointing at the given
// resolved chart OCI url + tag) and a HelmRelease referencing it via chartRef, then
// reports readiness. It is shared by the flux OCI path and the ocm path (which first
// resolves the OCI url + version from an OCM Resource status).
func (r *DeploySubroutine) reconcileResolvedOCIChart(ctx context.Context, namespace, name, ociURL, version string, insecure bool, values map[string]interface{}, runtimeKubeconfigSecretName string) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", DeploySubroutineName).ChildLogger("component", name)

	ociRepo := &unstructured.Unstructured{}
	ociRepo.SetGroupVersionKind(deployOCIRepoGVK)
	ociRepo.SetName(name)
	ociRepo.SetNamespace(namespace)
	ociResult, err := controllerutil.CreateOrUpdate(ctx, r.client, ociRepo, func() error {
		if err := unstructured.SetNestedField(ociRepo.Object, ociURL, "spec", "url"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(ociRepo.Object, version, "spec", "ref", "tag"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(ociRepo.Object, "generic", "spec", "provider"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(ociRepo.Object, "1m0s", "spec", "interval"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(ociRepo.Object, insecure, "spec", "insecure"); err != nil {
			return err
		}
		return unstructured.SetNestedMap(ociRepo.Object, map[string]interface{}{
			"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
			"operation": "copy",
		}, "spec", "layerSelector")
	})
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to reconcile OCIRepository %s/%s", namespace, name)
	}

	helmRelease := &unstructured.Unstructured{}
	helmRelease.SetGroupVersionKind(deployHelmReleaseGVK)
	helmRelease.SetName(name)
	helmRelease.SetNamespace(namespace)
	hrResult, err := controllerutil.CreateOrUpdate(ctx, r.client, helmRelease, func() error {
		if err := unstructured.SetNestedField(helmRelease.Object, "5m", "spec", "interval"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(helmRelease.Object, "OCIRepository", "spec", "chartRef", "kind"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(helmRelease.Object, name, "spec", "chartRef", "name"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(helmRelease.Object, namespace, "spec", "chartRef", "namespace"); err != nil {
			return err
		}
		if runtimeKubeconfigSecretName != "" {
			// The user requests to deploy this in a different runtime cluster.
			if err := unstructured.SetNestedMap(helmRelease.Object, map[string]interface{}{
				"name": runtimeKubeconfigSecretName,
				"key":  "kubeconfig",
			}, "spec", "kubeConfig", "secretRef"); err != nil {
				return err
			}
		}
		if values != nil {
			return unstructured.SetNestedMap(helmRelease.Object, values, "spec", "values")
		}
		return nil
	})
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to reconcile HelmRelease %s/%s", namespace, name)
	}

	// If either resource was just created or updated, skip condition checks — any existing
	// Ready=True on the HelmRelease is stale with respect to the new spec.
	if ociResult != controllerutil.OperationResultNone || hrResult != controllerutil.OperationResultNone {
		log.Info().Str("ociResult", string(ociResult)).Str("hrResult", string(hrResult)).Msg("Resource modified, requeuing before checking conditions")
		return subroutines.StopWithRequeue(deployRequeueDuration, fmt.Sprintf("waiting for %s to be reconciled", name)), nil
	}

	// Verify Flux has fully processed the current OCIRepository spec generation before
	// trusting HelmRelease conditions. A mismatch means Flux hasn't fetched the new
	// artifact yet, so the HelmRelease conditions still reflect the previous version.
	ociGeneration := ociRepo.GetGeneration()
	ociObservedGeneration, _, _ := unstructured.NestedInt64(ociRepo.Object, "status", "observedGeneration")
	if ociGeneration != ociObservedGeneration {
		log.Info().Int64("generation", ociGeneration).Int64("observedGeneration", ociObservedGeneration).Msg("OCIRepository not yet reconciled, requeuing")
		return subroutines.StopWithRequeue(deployRequeueDuration, fmt.Sprintf("waiting for OCIRepository %s/%s to be reconciled", namespace, name)), nil
	}

	ready, err := r.helmReleaseReady(ctx, namespace, name)
	if err != nil {
		return subroutines.OK(), err
	}
	if !ready {
		log.Info().Msg("HelmRelease not ready yet, requeuing")
		return subroutines.StopWithRequeue(deployRequeueDuration, fmt.Sprintf("HelmRelease %s not ready", name)), nil
	}

	return subroutines.OK(), nil
}

// parseOCMValues unmarshals an OCM component's Helm values, returning nil when none are set.
func parseOCMValues(ocm *providersv1alpha1.OCMComponentSpec) (map[string]interface{}, error) {
	if len(ocm.Values.Raw) == 0 {
		return nil, nil
	}
	var values map[string]interface{}
	if err := json.Unmarshal(ocm.Values.Raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}

// ocmResolvedOCIURL turns an OCM-resolved imageReference (and version) into a clean
// oci://host/repository URL suitable for a Flux OCIRepository spec.url. Mirrors the
// resolution done by the PlatformMesh ResourceSubroutine.
func ocmResolvedOCIURL(imageRef, version string) (string, error) {
	url := "oci://" + strings.TrimPrefix(imageRef, "oci://")
	url = strings.TrimSuffix(url, ":"+version)
	spec, err := ocm.ParseRef(url)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s/%s", spec.Scheme, spec.Host, spec.Repository), nil
}

// ocmConfigRepositoryRef returns the inline ocmConfig entry that points OCM objects at
// the generated Repository.
func ocmConfigRepositoryRef(name, namespace string) []interface{} {
	return []interface{}{
		map[string]interface{}{
			"apiVersion": deployOCMRepositoryGVK.GroupVersion().String(),
			"kind":       "Repository",
			"name":       name,
			"namespace":  namespace,
			// Must match the CRD-defaulted value, otherwise CreateOrUpdate clobbers the
			// server-defaulted policy on every reconcile and never converges (the Component
			// and Resource report "updated" forever, blocking HelmRelease creation).
			"policy": "Propagate",
		},
	}
}

// deployOCMComponent creates the delivery.ocm.software Repository, Component and Resource
// objects from the inline OCM coordinates. The ocm-controller resolves the descriptor and
// writes the resolved chart artifact (imageReference + version) into the Resource status,
// which is then deployed via a Flux OCIRepository + HelmRelease. All three OCM objects are
// named after the deployment name.
func (r *DeploySubroutine) deployOCMComponent(ctx context.Context, namespace, name string, ocmSpec *providersv1alpha1.OCMComponentSpec, runtimeKubeconfigSecretName string) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", DeploySubroutineName).ChildLogger("component", name)

	resourceName := ocmSpec.ResourceName
	if resourceName == "" {
		resourceName = defaultOCMResourceName
	}

	referencePath := make([]interface{}, 0, len(ocmSpec.ReferencePath))
	for _, elem := range ocmSpec.ReferencePath {
		referencePath = append(referencePath, map[string]interface{}{"name": elem.Name})
	}

	// 1. Repository — the OCI registry holding the component.
	baseURL, subPath := splitRegistry(ocmSpec.Registry)
	repository := &unstructured.Unstructured{}
	repository.SetGroupVersionKind(deployOCMRepositoryGVK)
	repository.SetName(name)
	repository.SetNamespace(namespace)
	repoResult, err := controllerutil.CreateOrUpdate(ctx, r.client, repository, func() error {
		if err := unstructured.SetNestedField(repository.Object, "1m0s", "spec", "interval"); err != nil {
			return err
		}
		return unstructured.SetNestedMap(repository.Object, map[string]interface{}{
			"type":    "OCIRegistry",
			"baseUrl": baseURL,
			"subPath": subPath,
		}, "spec", "repositorySpec")
	})
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to reconcile Repository %s/%s", namespace, name)
	}

	// 2. Component — the OCM component + version within the repository.
	component := &unstructured.Unstructured{}
	component.SetGroupVersionKind(deployOCMComponentGVK)
	component.SetName(name)
	component.SetNamespace(namespace)
	compResult, err := controllerutil.CreateOrUpdate(ctx, r.client, component, func() error {
		if err := unstructured.SetNestedField(component.Object, ocmSpec.Component, "spec", "component"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(component.Object, ocmSpec.Version, "spec", "semver"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(component.Object, "1m0s", "spec", "interval"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(component.Object, "Deny", "spec", "downgradePolicy"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(component.Object, name, "spec", "repositoryRef", "name"); err != nil {
			return err
		}
		return unstructured.SetNestedSlice(component.Object, ocmConfigRepositoryRef(name, namespace), "spec", "ocmConfig")
	})
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to reconcile Component %s/%s", namespace, name)
	}

	// 3. Resource — selects the chart resource within the component.
	resource := &unstructured.Unstructured{}
	resource.SetGroupVersionKind(deployOCMResourceGVK)
	resource.SetName(name)
	resource.SetNamespace(namespace)
	resResult, err := controllerutil.CreateOrUpdate(ctx, r.client, resource, func() error {
		labels := resource.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels["artifact"] = "chart"
		labels["repo"] = "oci"
		resource.SetLabels(labels)

		if err := unstructured.SetNestedField(resource.Object, name, "spec", "componentRef", "name"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(resource.Object, resourceName, "spec", "resource", "byReference", "resource", "name"); err != nil {
			return err
		}
		if len(referencePath) > 0 {
			if err := unstructured.SetNestedSlice(resource.Object, referencePath, "spec", "resource", "byReference", "referencePath"); err != nil {
				return err
			}
		}
		return unstructured.SetNestedSlice(resource.Object, ocmConfigRepositoryRef(name, namespace), "spec", "ocmConfig")
	})
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to reconcile Resource %s/%s", namespace, name)
	}

	// If any OCM object was just created or updated, give the ocm-controller time to
	// (re)resolve before trusting the Resource status.
	if repoResult != controllerutil.OperationResultNone || compResult != controllerutil.OperationResultNone || resResult != controllerutil.OperationResultNone {
		log.Info().
			Str("repoResult", string(repoResult)).Str("compResult", string(compResult)).Str("resResult", string(resResult)).
			Msg("OCM objects modified, requeuing for resolution")
		return subroutines.StopWithRequeue(deployRequeueDuration, fmt.Sprintf("waiting for OCM Resource %s to be resolved", name)), nil
	}

	// Read the resolved artifact from the Resource status (populated by the ocm-controller).
	imageRef, _, _ := unstructured.NestedString(resource.Object, "status", "resource", "access", "imageReference")
	if imageRef == "" {
		imageRef, _, _ = unstructured.NestedString(resource.Object, "status", "resource", "imageReference")
	}
	version, _, _ := unstructured.NestedString(resource.Object, "status", "resource", "version")
	if imageRef == "" || version == "" {
		log.Info().Msg("OCM Resource not yet resolved, requeuing")
		return subroutines.StopWithRequeue(deployRequeueDuration, fmt.Sprintf("waiting for OCM Resource %s status", name)), nil
	}

	ociURL, err := ocmResolvedOCIURL(imageRef, version)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to parse resolved imageReference for %s", name)
	}

	values, err := parseOCMValues(ocmSpec)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to unmarshal values for %s", name)
	}

	return r.reconcileResolvedOCIChart(ctx, namespace, name, ociURL, version, ocmSpec.Insecure, values, runtimeKubeconfigSecretName)
}

// deployFluxHelmRepo deploys a chart from a classic HTTP(S) Helm repository via a
// Flux HelmRepository referenced by a HelmRelease through chart.spec.sourceRef.
func (r *DeploySubroutine) deployFluxHelmRepo(ctx context.Context, namespace, name string, flux *providersv1alpha1.FluxComponentSpec, runtimeKubeconfigSecretName string) (subroutines.Result, error) {
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", DeploySubroutineName).ChildLogger("component", name)

	helmRepo := &unstructured.Unstructured{}
	helmRepo.SetGroupVersionKind(deployHelmRepoGVK)
	helmRepo.SetName(name)
	helmRepo.SetNamespace(namespace)
	repoResult, err := controllerutil.CreateOrUpdate(ctx, r.client, helmRepo, func() error {
		if err := unstructured.SetNestedField(helmRepo.Object, flux.Registry, "spec", "url"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(helmRepo.Object, "5m", "spec", "interval"); err != nil {
			return err
		}
		return unstructured.SetNestedField(helmRepo.Object, flux.Insecure, "spec", "insecure")
	})
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to reconcile HelmRepository %s/%s", namespace, name)
	}

	values, err := parseFluxValues(flux)
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to unmarshal values for %s", name)
	}

	helmRelease := &unstructured.Unstructured{}
	helmRelease.SetGroupVersionKind(deployHelmReleaseGVK)
	helmRelease.SetName(name)
	helmRelease.SetNamespace(namespace)
	hrResult, err := controllerutil.CreateOrUpdate(ctx, r.client, helmRelease, func() error {
		if err := unstructured.SetNestedField(helmRelease.Object, "5m", "spec", "interval"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(helmRelease.Object, flux.Chart, "spec", "chart", "spec", "chart"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(helmRelease.Object, flux.Version, "spec", "chart", "spec", "version"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(helmRelease.Object, "HelmRepository", "spec", "chart", "spec", "sourceRef", "kind"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(helmRelease.Object, name, "spec", "chart", "spec", "sourceRef", "name"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(helmRelease.Object, namespace, "spec", "chart", "spec", "sourceRef", "namespace"); err != nil {
			return err
		}
		if runtimeKubeconfigSecretName != "" {
			// The user requests to deploy this in a different runtime cluster.
			if err := unstructured.SetNestedMap(helmRelease.Object, map[string]interface{}{
				"name": runtimeKubeconfigSecretName,
				"key":  "kubeconfig",
			}, "spec", "kubeConfig", "secretRef"); err != nil {
				return err
			}
		}
		if values != nil {
			return unstructured.SetNestedMap(helmRelease.Object, values, "spec", "values")
		}
		return nil
	})
	if err != nil {
		return subroutines.OK(), gcerrors.Wrap(err, "failed to reconcile HelmRelease %s/%s", namespace, name)
	}

	// If either resource was just created or updated, skip condition checks — any existing
	// Ready=True on the HelmRelease is stale with respect to the new spec.
	if repoResult != controllerutil.OperationResultNone || hrResult != controllerutil.OperationResultNone {
		log.Info().Str("repoResult", string(repoResult)).Str("hrResult", string(hrResult)).Msg("Resource modified, requeuing before checking conditions")
		return subroutines.StopWithRequeue(deployRequeueDuration, fmt.Sprintf("waiting for %s to be reconciled", name)), nil
	}

	ready, err := r.helmReleaseReady(ctx, namespace, name)
	if err != nil {
		return subroutines.OK(), err
	}
	if !ready {
		log.Info().Msg("HelmRelease not ready yet, requeuing")
		return subroutines.StopWithRequeue(deployRequeueDuration, fmt.Sprintf("HelmRelease %s not ready", name)), nil
	}

	return subroutines.OK(), nil
}

func (r *DeploySubroutine) helmReleaseReady(ctx context.Context, namespace, name string) (bool, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(deployHelmReleaseGVK)
	if err := r.client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj); err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, gcerrors.Wrap(err, "failed to get HelmRelease %s/%s", namespace, name)
	}
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] == "Ready" && cond["status"] == "True" {
			return true, nil
		}
	}
	return false, nil
}

func (r *DeploySubroutine) Finalize(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	inst := obj.(*providersv1alpha1.ManagedProvider)
	log := logger.LoadLoggerFromContext(ctx).ChildLogger("subroutine", r.GetName())

	inst.Status.Phase = providersv1alpha1.ManagedProviderPhaseDeleting

	needsDeletion := false
	// deleteObj deletes a single object by GVK+name, reporting whether it existed
	// (so the finalizer can requeue until everything is gone).
	deleteObj := func(gvk schema.GroupVersionKind, name string) (bool, error) {
		o := &unstructured.Unstructured{}
		o.SetGroupVersionKind(gvk)
		o.SetName(name)
		o.SetNamespace(inst.Namespace)
		if err := r.client.Delete(ctx, o); err != nil {
			if !kerrors.IsNotFound(err) {
				return false, gcerrors.Wrap(err, "failed to delete %s %s/%s", gvk.Kind, inst.Namespace, name)
			}
			return false, nil
		}
		return true, nil
	}

	for _, component := range inst.Spec.RuntimeDeployments {
		var gvks []schema.GroupVersionKind
		var name string
		switch {
		case component.Flux != nil:
			name = chartResourceName(component.Flux.Chart)
			gvks = []schema.GroupVersionKind{deployHelmReleaseGVK, fluxSourceGVK(component.Flux)}
		case component.OCM != nil:
			name = ocmDeploymentName(component.OCM)
			gvks = []schema.GroupVersionKind{deployHelmReleaseGVK, deployOCIRepoGVK, deployOCMResourceGVK, deployOCMComponentGVK, deployOCMRepositoryGVK}
		default:
			continue
		}
		for _, gvk := range gvks {
			deleted, err := deleteObj(gvk, name)
			if err != nil {
				return subroutines.OK(), err
			}
			needsDeletion = needsDeletion || deleted
		}
	}

	if !needsDeletion {
		log.Info().Msg("Deleted all Flux resources")
		r.limiter.Forget(inst)
		return subroutines.OK(), nil
	}

	return subroutines.StopWithRequeue(r.limiter.When(inst), "Waiting for Flux resources to be deleted"), nil
}

func (r *DeploySubroutine) Finalizers(_ client.Object) []string {
	return []string{deploySubroutineFinalizer}
}
