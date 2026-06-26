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

package resolver

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/graphql-go/graphql"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/metrics"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Service struct {
	runtimeClient ctrlruntimeclient.WithWatch
	metrics       *metrics.ResolverMetrics
}

func New(runtimeClient ctrlruntimeclient.WithWatch, m *metrics.ResolverMetrics) *Service {
	return &Service{
		runtimeClient: runtimeClient,
		metrics:       m,
	}
}

func (r *Service) ListItems(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (result any, err error) {
		start := time.Now()
		defer func() {
			if r.metrics == nil {
				return
			}
			labelResult := metrics.ResultSuccess
			if err != nil {
				labelResult = metrics.ResultError
			}
			r.metrics.Record(metrics.OperationList, gvk.Kind, time.Since(start), labelResult)
		}()
		logger := log.FromContext(p.Context)
		ctx, span := otel.Tracer("").Start(p.Context, "ListItems", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		logger = logger.WithValues(
			"operation", "list",
			"group", gvk.Group,
			"version", gvk.Version,
			"kind", gvk.Kind,
		)

		// Create an unstructured list to hold the results
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)

		var opts []ctrlruntimeclient.ListOption

		labelSelector, err := GetArg[string](p.Args, LabelSelectorArg, false)
		if err != nil {
			return nil, err
		}
		if labelSelector != "" {
			selector, err := labels.Parse(labelSelector)
			if err != nil {
				logger.WithValues(LabelSelectorArg, labelSelector).Error(err, "Unable to parse given label selector")
				return nil, err
			}
			opts = append(opts, ctrlruntimeclient.MatchingLabelsSelector{Selector: selector})
		}

		if isResourceNamespaceScoped(scope) {
			namespace, err := GetArg[string](p.Args, NamespaceArg, false)
			if err != nil {
				return nil, err
			}
			if namespace != "" {
				opts = append(opts, ctrlruntimeclient.InNamespace(namespace))
			}
		}

		limit, err := GetArg[int](p.Args, LimitArg, false)
		if err != nil {
			return nil, err
		}
		if limit > 0 {
			opts = append(opts, ctrlruntimeclient.Limit(int64(limit)))
		}

		continueToken, err := GetArg[string](p.Args, ContinueArg, false)
		if err != nil {
			return nil, err
		}
		if continueToken != "" {
			opts = append(opts, ctrlruntimeclient.Continue(continueToken))
		}

		if err = r.runtimeClient.List(ctx, list, opts...); err != nil {
			logger.Error(err, "Unable to list objects")
			return nil, fmt.Errorf("unable to list objects: %w", err)
		}

		sortBy, err := GetArg[string](p.Args, SortByArg, false)
		if err != nil {
			return nil, err
		}

		if sortBy != "" {
			if err := validateSortBy(list.Items, sortBy); err != nil {
				logger.WithValues(SortByArg, sortBy).Error(err, "Invalid sortBy field path")
				return nil, err
			}
			slices.SortFunc(list.Items, compareUnstructured(sortBy))
		}

		items := make([]map[string]any, len(list.Items))
		for i, item := range list.Items {
			items[i] = item.Object
		}

		return &ListResult{
			ResourceVersion:    list.GetResourceVersion(),
			Items:              items,
			Continue:           list.GetContinue(),
			RemainingItemCount: list.GetRemainingItemCount(),
		}, nil
	}
}

func (r *Service) GetItem(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (result any, err error) {
		start := time.Now()
		defer func() {
			if r.metrics == nil {
				return
			}
			labelResult := metrics.ResultSuccess
			if err != nil {
				labelResult = metrics.ResultError
			}
			r.metrics.Record(metrics.OperationGet, gvk.Kind, time.Since(start), labelResult)
		}()
		logger := log.FromContext(p.Context)
		ctx, span := otel.Tracer("").Start(p.Context, "GetItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		logger = logger.WithValues(
			"operation", "get",
			"group", gvk.Group,
			"version", gvk.Version,
			"kind", gvk.Kind,
		)

		// Retrieve required arguments
		name, err := GetArg[string](p.Args, NameArg, true)
		if err != nil {
			return nil, err
		}

		// Create an unstructured object to hold the result
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)

		key := ctrlruntimeclient.ObjectKey{
			Name: name,
		}

		if isResourceNamespaceScoped(scope) {
			namespace, err := GetArg[string](p.Args, NamespaceArg, true)
			if err != nil {
				return nil, err
			}

			key.Namespace = namespace
		}

		// Get the object using the runtime client
		if err = r.runtimeClient.Get(ctx, key, obj); err != nil {
			logger.WithValues("name", name, "scope", string(scope)).Error(err, "Unable to get object")
			return nil, err
		}

		return obj.Object, nil
	}
}

func (r *Service) GetItemAsYAML(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (result any, err error) {
		_, span := otel.Tracer("").Start(p.Context, "GetItemAsYAML", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		out, err := r.GetItem(gvk, scope)(p)
		if err != nil {
			return "", err
		}

		var returnYaml bytes.Buffer
		if err = yaml.NewEncoder(&returnYaml).Encode(out); err != nil {
			return "", err
		}

		return returnYaml.String(), nil
	}
}

func (r *Service) CreateItem(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (result any, err error) {
		start := time.Now()
		defer func() {
			if r.metrics == nil {
				return
			}
			labelResult := metrics.ResultSuccess
			if err != nil {
				labelResult = metrics.ResultError
			}
			r.metrics.Record(metrics.OperationCreate, gvk.Kind, time.Since(start), labelResult)
		}()
		ctx, span := otel.Tracer("").Start(p.Context, "CreateItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		logger := log.FromContext(p.Context).WithValues(
			"operation", "create",
			"group", gvk.Group,
			"version", gvk.Version,
			"kind", gvk.Kind,
		)

		objectInput := p.Args["object"].(map[string]any)

		obj := &unstructured.Unstructured{
			Object: objectInput,
		}
		obj.SetGroupVersionKind(gvk)

		if isResourceNamespaceScoped(scope) {
			namespace, err := GetArg[string](p.Args, NamespaceArg, true)
			if err != nil {
				return nil, err
			}
			obj.SetNamespace(namespace)
		}

		if obj.GetName() == "" && obj.GetGenerateName() == "" {
			return nil, errors.New("object metadata.name or metadata.generateName is required")
		}

		dryRunBool, err := GetArg[bool](p.Args, DryRunArg, false)
		if err != nil {
			return nil, err
		}
		dryRun := []string{}
		if dryRunBool {
			dryRun = []string{"All"}
		}

		if err := r.runtimeClient.Create(ctx, obj, &ctrlruntimeclient.CreateOptions{DryRun: dryRun}); err != nil {
			logger.Error(err, "Failed to create object")
			return nil, err
		}

		return obj.Object, nil
	}
}

func (r *Service) UpdateItem(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (result any, err error) {
		start := time.Now()
		defer func() {
			if r.metrics == nil {
				return
			}
			labelResult := metrics.ResultSuccess
			if err != nil {
				labelResult = metrics.ResultError
			}
			r.metrics.Record(metrics.OperationUpdate, gvk.Kind, time.Since(start), labelResult)
		}()
		logger := log.FromContext(p.Context)
		ctx, span := otel.Tracer("").Start(p.Context, "UpdateItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		logger = logger.WithValues("operation", "update", "kind", gvk.Kind)

		name, err := GetArg[string](p.Args, NameArg, true)
		if err != nil {
			return nil, err
		}

		objectInput := p.Args[ObjectArg].(map[string]any)
		patchData, err := json.Marshal(objectInput)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal object input: %w", err)
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		obj.SetName(name)

		if isResourceNamespaceScoped(scope) {
			namespace, err := GetArg[string](p.Args, NamespaceArg, true)
			if err != nil {
				return nil, err
			}
			obj.SetNamespace(namespace)
		}

		dryRunBool, err := GetArg[bool](p.Args, DryRunArg, false)
		if err != nil {
			return nil, err
		}
		var dryRun []string
		if dryRunBool {
			dryRun = []string{"All"}
		}

		patch := ctrlruntimeclient.RawPatch(types.MergePatchType, patchData)
		if err := r.runtimeClient.Patch(ctx, obj, patch, &ctrlruntimeclient.PatchOptions{DryRun: dryRun}); err != nil {
			logger.Error(err, "Failed to patch object")
			return nil, err
		}

		return obj.Object, nil
	}
}

func (r *Service) DeleteItem(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (result any, err error) {
		start := time.Now()
		defer func() {
			if r.metrics == nil {
				return
			}
			labelResult := metrics.ResultSuccess
			if err != nil {
				labelResult = metrics.ResultError
			}
			r.metrics.Record(metrics.OperationDelete, gvk.Kind, time.Since(start), labelResult)
		}()
		logger := log.FromContext(p.Context)
		ctx, span := otel.Tracer("").Start(p.Context, "DeleteItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		logger = logger.WithValues("operation", "delete", "kind", gvk.Kind)

		name, err := GetArg[string](p.Args, NameArg, true)
		if err != nil {
			return nil, err
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		obj.SetName(name)

		if isResourceNamespaceScoped(scope) {
			namespace, err := GetArg[string](p.Args, NamespaceArg, true)
			if err != nil {
				return nil, err
			}
			obj.SetNamespace(namespace)
		}

		dryRunBool, err := GetArg[bool](p.Args, DryRunArg, false)
		if err != nil {
			return nil, err
		}
		dryRun := []string{}
		if dryRunBool {
			dryRun = []string{"All"}
		}

		if err := r.runtimeClient.Delete(ctx, obj, &ctrlruntimeclient.DeleteOptions{DryRun: dryRun}); err != nil {
			logger.Error(err, "Failed to delete object")
			return nil, err
		}

		return true, nil
	}
}

// ApplyYaml returns a resolver that applies a single YAML document to the
// Kubernetes API server with create-or-update semantics: if the resource
// exists it is updated, otherwise it is created.
func (r *Service) ApplyYaml() graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (result any, err error) {
		start := time.Now()
		var kind string
		defer func() {
			if r.metrics == nil {
				return
			}
			labelResult := metrics.ResultSuccess
			if err != nil {
				labelResult = metrics.ResultError
			}
			r.metrics.Record(metrics.OperationApply, kind, time.Since(start), labelResult)
		}()
		ctx, span := otel.Tracer("").Start(p.Context, "ApplyYaml")
		defer span.End()

		logger := log.FromContext(ctx).WithValues("operation", "apply")

		yamlStr, err := GetArg[string](p.Args, YamlArg, true)
		if err != nil {
			return nil, err
		}

		parsed, err := parseAndValidateYAML(yamlStr)
		if err != nil {
			return nil, err
		}

		obj := &unstructured.Unstructured{Object: parsed}

		gvk := obj.GetObjectKind().GroupVersionKind()
		kind = gvk.Kind
		name := obj.GetName()
		namespace := obj.GetNamespace()

		span.SetAttributes(
			attribute.String("kind", gvk.Kind),
			attribute.String("name", name),
		)

		logger = logger.WithValues(
			"group", gvk.Group,
			"version", gvk.Version,
			"kind", gvk.Kind,
			"name", name,
			"namespace", namespace,
		)

		if name == "" {
			if err := r.runtimeClient.Create(ctx, obj); err != nil {
				logger.Error(err, "Failed to create object with generateName")
				return nil, fmt.Errorf("failed to create resource %s: %w", gvk.Kind, err)
			}
			return obj.Object, nil
		}

		target := &unstructured.Unstructured{}
		target.SetGroupVersionKind(gvk)
		target.SetName(name)
		target.SetNamespace(namespace)

		if _, err := controllerutil.CreateOrUpdate(ctx, r.runtimeClient, target, func() error {
			rv := target.GetResourceVersion()
			uid := target.GetUID()
			target.Object = parsed
			target.SetResourceVersion(rv)
			target.SetUID(uid)
			return nil
		}); err != nil {
			logger.Error(err, "Failed to apply YAML")
			return nil, fmt.Errorf("failed to apply resource %s/%s: %w", gvk.Kind, name, err)
		}

		return target.Object, nil
	}
}

func (r *Service) CommonResolver() graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		return p.Source, nil
	}
}

func compareUnstructured(fieldPath string) func(a, b unstructured.Unstructured) int {
	return func(a, b unstructured.Unstructured) int {
		segments := strings.Split(fieldPath, ".")

		aVal, foundA, errA := unstructured.NestedFieldNoCopy(a.Object, segments...)
		bVal, foundB, errB := unstructured.NestedFieldNoCopy(b.Object, segments...)
		if errA != nil || errB != nil || !foundA || !foundB {
			return 0
		}

		switch av := aVal.(type) {
		case string:
			return cmp.Compare(av, bVal.(string))
		case float64:
			return cmp.Compare(av, bVal.(float64))
		case int64:
			return cmp.Compare(av, bVal.(int64))
		case bool:
			// bool not in cmp.Ordered; false < true
			if av == bVal.(bool) {
				return 0
			} else if bVal.(bool) {
				return -1
			}
			return 1
		}

		return 0
	}
}

// parseAndValidateYAML decodes a YAML string, rejects multi-document input,
// and validates that apiVersion, kind, and metadata.name or metadata.generateName are present.
func parseAndValidateYAML(yamlStr string) (map[string]any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader([]byte(yamlStr)))

	var parsed map[string]any
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("multi-document YAML is not supported; provide a single document")
	}

	apiVersion, ok := parsed["apiVersion"].(string)
	if !ok || apiVersion == "" {
		return nil, errors.New("apiVersion is required and must be a string")
	}

	kind, ok := parsed["kind"].(string)
	if !ok || kind == "" {
		return nil, errors.New("kind is required and must be a string")
	}

	metadata, ok := parsed["metadata"].(map[string]any)
	if !ok || metadata == nil {
		return nil, errors.New("metadata is required")
	}
	name, _ := metadata["name"].(string)
	generateName, _ := metadata["generateName"].(string)
	if name == "" && generateName == "" {
		return nil, errors.New("metadata.name or metadata.generateName is required")
	}

	return parsed, nil
}
