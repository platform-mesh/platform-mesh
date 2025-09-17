package subroutine

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	lifecycleruntimeobject "github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
)

var (
	//go:embed manifests/organizationIdp/repository.yaml
	repository string

	//go:embed manifests/organizationIdp/helmrelease.yaml
	helmRelease string
)

type realmSubroutine struct {
	k8s        client.Client
	baseDomain string
}

func NewRealmSubroutine(k8s client.Client, baseDomain string) *realmSubroutine {
	return &realmSubroutine{
		k8s,
		baseDomain,
	}
}

var _ lifecyclesubroutine.Subroutine = &realmSubroutine{}

func (r *realmSubroutine) GetName() string { return "Realm" }

func (r *realmSubroutine) Finalizers() []string { return []string{} }

func (r *realmSubroutine) Finalize(ctx context.Context, instance lifecycleruntimeobject.RuntimeObject) (reconcile.Result, errors.OperatorError) {
	log := logger.LoadLoggerFromContext(ctx)

	lc := instance.(*kcpv1alpha1.LogicalCluster)
	workspaceName := getWorkspaceName(lc)
	if workspaceName == "" {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get workspace path"), true, false)
	}

	ociObj, err := unstructuredFromString(repository, nil, log)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to load OCI repository manifest: %w", err), true, true)
	}
	if err := r.k8s.Delete(ctx, &ociObj); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to delete OCI repository: %w", err), true, true)
	}

	helmObj, err := unstructuredFromString(helmRelease, nil, log)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to load HelmRelease  manifest: %w", err), true, true)
	}
	helmObj.SetName(workspaceName)
	if err := r.k8s.Delete(ctx, &helmObj); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to delete HelmRelease: %w", err), true, true)
	}

	log.Info().Str("realm", workspaceName).Msg("Successfully finalized resources")
	return ctrl.Result{}, nil
}

func (r *realmSubroutine) Process(ctx context.Context, instance lifecycleruntimeobject.RuntimeObject) (reconcile.Result, errors.OperatorError) {
	lc := instance.(*kcpv1alpha1.LogicalCluster)

	workspaceName := getWorkspaceName(lc)
	if workspaceName == "" {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get workspace path"), true, false)
	}

	patch := map[string]interface{}{
		"crossplane": map[string]interface{}{
			"realm": map[string]interface{}{
				"name":        workspaceName,
				"displayName": workspaceName,
			},
			"client": map[string]interface{}{
				"name":        workspaceName,
				"displayName": workspaceName,
				"validRedirectUris": []string{
					fmt.Sprintf("https://%s.%s/callback*", workspaceName, r.baseDomain),
				},
			},
		},
		"keycloakConfig": map[string]interface{}{
			"client": map[string]interface{}{
				"name": workspaceName,
				"targetSecret": map[string]interface{}{
					"name": fmt.Sprintf("portal-client-secret-%s", workspaceName),
				},
			},
		},
	}

	marshalledPatch, err := json.Marshal(patch)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to marshall patch map: %w", err), true, true)
	}

	values := apiextensionsv1.JSON{Raw: marshalledPatch}
	releaseName := fmt.Sprintf("%s-idp", workspaceName)

	err = applyManifestWithMergedValues(ctx, repository, r.k8s, nil)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to create OCI repository: %w", err), true, true)
	}

	err = applyReleaseWithValues(ctx, helmRelease, r.k8s, values, releaseName)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to create HelmRelease: %w", err), true, true)
	}

	return ctrl.Result{}, nil
}

func getWorkspaceName(lc *kcpv1alpha1.LogicalCluster) string {
	if path, ok := lc.Annotations["kcp.io/path"]; ok {
		pathElements := strings.Split(path, ":")
		return pathElements[len(pathElements)-1]
	}
	return ""
}

func applyReleaseWithValues(ctx context.Context, release string, k8sClient client.Client, values apiextensionsv1.JSON, releaseName string) error {
	log := logger.LoadLoggerFromContext(ctx)

	obj, err := unstructuredFromString(release, map[string]string{}, log)
	if err != nil {
		return errors.Wrap(err, "Failed to get unstructuredFromFile")
	}
	obj.SetName(releaseName)

	if err := unstructured.SetNestedField(obj.Object, releaseName, "spec", "releaseName"); err != nil {
		return errors.Wrap(err, "failed to set spec.releaseName")
	}

	obj.Object["spec"].(map[string]interface{})["values"] = values

	err = k8sClient.Patch(ctx, &obj, client.Apply, client.FieldOwner("security-operator"))
	if err != nil {
		return errors.Wrap(err, "Failed to apply manifest: (%s/%s)", obj.GetKind(), obj.GetName())
	}
	return nil
}

func unstructuredFromString(manifest string, templateData map[string]string, log *logger.Logger) (unstructured.Unstructured, error) {
	manifestBytes := []byte(manifest)

	res, err := ReplaceTemplate(templateData, manifestBytes)
	if err != nil {
		return unstructured.Unstructured{}, errors.Wrap(err, "Failed to replace template")
	}

	var objMap map[string]interface{}
	if err := yaml.Unmarshal(res, &objMap); err != nil {
		return unstructured.Unstructured{}, errors.Wrap(err, "Failed to unmarshal YAML from template. Output:\n%s", string(res))
	}

	log.Debug().Str("obj", fmt.Sprintf("%+v", objMap)).Msg("Unmarshalled object")

	obj := unstructured.Unstructured{Object: objMap}

	log.Debug().Str("kind", obj.GetKind()).Str("name", obj.GetName()).Str("namespace", obj.GetNamespace()).Msg("Applying manifest")
	return obj, err
}

func ReplaceTemplate(templateData map[string]string, templateBytes []byte) ([]byte, error) {
	tmpl, err := template.New("manifest").Parse(string(templateBytes))
	if err != nil {
		return []byte{}, errors.Wrap(err, "Failed to parse template")
	}
	var result bytes.Buffer
	err = tmpl.Execute(&result, templateData)
	if err != nil {
		keys := make([]string, 0, len(templateData))
		for k := range templateData {
			keys = append(keys, k)
		}
		return []byte{}, errors.Wrap(err, "Failed to execute template with keys %v", keys)
	}
	if result.Len() == 0 {
		return []byte{}, nil
	}
	return result.Bytes(), nil
}

func applyManifestWithMergedValues(ctx context.Context, manifest string, k8sClient client.Client, templateData map[string]string) error {
	log := logger.LoadLoggerFromContext(ctx)

	obj, err := unstructuredFromString(manifest, templateData, log)
	if err != nil {
		return err
	}

	err = k8sClient.Patch(ctx, &obj, client.Apply, client.FieldOwner("security-operator"))
	if err != nil {
		return errors.Wrap(err, "Failed to apply manifest (%s/%s)", obj.GetKind(), obj.GetName())
	}
	return nil
}
