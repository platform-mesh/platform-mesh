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

package apischema

import (
	"context"
	"encoding/json"
	"errors"
	"maps"

	"go.platform-mesh.io/kubernetes-graphql-gateway/apischema"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/schemamutation"
	"k8s.io/kube-openapi/pkg/spec3"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	// ErrGetOpenAPIPaths indicates failure to retrieve OpenAPI paths from the API server.
	ErrGetOpenAPIPaths = errors.New("failed to get OpenAPI paths")
)

// SchemaLoader loads OpenAPI schemas from a Kubernetes API server.
type SchemaLoader struct{}

// NewSchemaLoader creates a new SchemaLoader.
func NewSchemaLoader() *SchemaLoader {
	return &SchemaLoader{}
}

// Load fetches and parses all OpenAPI schemas from the client.
// GVK is extracted once per schema via type assertion
func (l *SchemaLoader) Load(ctx context.Context, oc openapi.Client) (*apischema.SchemaSet, error) {
	logger := log.FromContext(ctx)

	paths, err := oc.Paths()
	if err != nil {
		return nil, errors.Join(ErrGetOpenAPIPaths, err)
	}

	entries := make(map[string]*apischema.SchemaEntry)
	walker := createRefWalker()

	for pathKey, path := range paths {
		pathEntries, errs := l.loadPath(ctx, path, walker)
		for _, e := range errs {
			logger.V(4).Info("error loading schema path",
				"path", pathKey,
				"error", e)
		}

		maps.Copy(entries, pathEntries)
	}

	logger.Info("loaded schemas", "count", len(entries))

	return apischema.NewSchemaSet(entries), nil
}

func (l *SchemaLoader) loadPath(
	ctx context.Context,
	path openapi.GroupVersion,
	walker schemamutation.Walker,
) (map[string]*apischema.SchemaEntry, []error) {
	logger := log.FromContext(ctx)
	entries := make(map[string]*apischema.SchemaEntry)
	var errs []error

	schemaBytes, err := path.Schema(discovery.AcceptV2)
	if err != nil {
		errs = append(errs, err)
		return entries, errs
	}

	var openAPISpec spec3.OpenAPI
	if err := json.Unmarshal(schemaBytes, &openAPISpec); err != nil {
		errs = append(errs, err)
		return entries, errs
	}

	if openAPISpec.Components == nil {
		return entries, errs
	}

	for key, schema := range openAPISpec.Components.Schemas {
		// Walk and normalize refs
		walked := walker.WalkSchema(schema)

		gvk, err := apischema.ExtractGVK(walked)
		if err != nil {
			logger.V(4).Info("failed to extract GVK",
				"key", key,
				"error", err)
			errs = append(errs, err)
			continue
		}

		entries[key] = &apischema.SchemaEntry{
			Key:    key,
			Schema: walked,
			GVK:    gvk,
		}
	}

	return entries, errs
}

// createRefWalker creates a schema walker that normalizes $ref pointers.
// This simplifies refs from full paths to short names.
func createRefWalker() schemamutation.Walker {
	return schemamutation.Walker{
		RefCallback: schemamutation.RefCallbackNoop,
		SchemaCallback: func(schema *spec.Schema) *spec.Schema {
			refPtr := schema.Ref.GetPointer()
			if refPtr == nil {
				return schema
			}

			tokens := refPtr.DecodedTokens()
			if len(tokens) == 0 {
				return schema
			}

			resolvedRef := tokens[len(tokens)-1]
			return &spec.Schema{
				SchemaProps: spec.SchemaProps{
					Ref: spec.MustCreateRef(resolvedRef),
				},
			}
		},
	}
}
