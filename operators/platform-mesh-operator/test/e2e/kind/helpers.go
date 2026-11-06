//go:build e2e

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

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"strings"
	"time"

	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func dynamicClientForKubeconfig(kubeconfigBytes []byte) (dynamic.Interface, error) {
	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, err
	}
	cfg.Timeout = 60 * time.Second
	return dynamic.NewForConfig(cfg)
}

// ApplyManifestFromFile renders the (possibly multi-document) manifest at path
// in fsys with templateData and server-side applies every object in it.
func ApplyManifestFromFile(
	ctx context.Context,
	fsys fs.FS, path string, k8sClient ctrlruntimeclient.Client, templateData map[string]string,
) error {
	log := logger.LoadLoggerFromContext(ctx)

	objs, err := unstructuredsFromFile(fsys, path, templateData, log)
	if err != nil {
		return err
	}

	var errRet error
	for _, obj := range objs {
		if obj.Object == nil {
			continue
		}
		err = k8sClient.Apply(ctx, ctrlruntimeclient.ApplyConfigurationFromUnstructured(&obj),
			ctrlruntimeclient.FieldOwner("platform-mesh-operator"))
		if err != nil {
			errRet = errors.Wrap(errRet, "Failed to apply manifest file: %s (%s/%s)", path, obj.GetKind(), obj.GetName())
		}
	}
	return errRet
}

func unstructuredsFromFile(fsys fs.FS, path string, templateData map[string]string, log *logger.Logger) ([]unstructured.Unstructured, error) {
	manifestBytes, err := fs.ReadFile(fsys, path)
	if err != nil {
		return []unstructured.Unstructured{}, errors.Wrap(err, "Failed to read file: %s", path)
	}
	log.Debug().Str("file", path).Str("template", string(manifestBytes)).Str("templateData", fmt.Sprintf("%+v", templateData)).Msg("Replacing template")

	res, err := ReplaceTemplate(templateData, manifestBytes)
	if err != nil {
		return []unstructured.Unstructured{}, errors.Wrap(err, "Failed to replace template with path: %s", path)
	}

	// split the result into multiple YAML objects
	objects := strings.Split(string(res), "---\n")
	var unstructuredObjs []unstructured.Unstructured
	for _, obj := range objects {
		var objMap map[string]any
		if err := yaml.Unmarshal([]byte(obj), &objMap); err != nil {
			return []unstructured.Unstructured{}, errors.Wrap(err, "Failed to unmarshal YAML from template %s. Output:\n%s", path, string(res))
		}

		log.Debug().Str("obj", fmt.Sprintf("%+v", objMap)).Msg("Unmarshalled object")

		obj := unstructured.Unstructured{Object: objMap}

		log.Debug().Str("file", path).Str("kind", obj.GetKind()).Str("name", obj.GetName()).Str("namespace", obj.GetNamespace()).Msg("Applying manifest")
		unstructuredObjs = append(unstructuredObjs, obj)
	}
	return unstructuredObjs, nil
}

func ReplaceTemplate(templateData map[string]string, templateBytes []byte) ([]byte, error) {
	// If no template data is provided, return the raw bytes unchanged.
	// This avoids the template engine interpreting {{ }} expressions that are meant to be
	// preserved as-is in the output (e.g. Go template expressions inside ConfigMap values
	// that the operator renders at runtime).
	if len(templateData) == 0 {
		return templateBytes, nil
	}
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
