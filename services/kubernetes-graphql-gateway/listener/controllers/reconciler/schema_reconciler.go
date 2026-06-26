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

package reconciler

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/schemahandler"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ErrCreateHTTPClient = errors.New("failed to create HTTP client")
	ErrCreateRESTMapper = errors.New("failed to create REST mapper")
)

type Reconciler struct {
	schemaHandler schemahandler.Handler
}

func NewReconciler(ioHandler schemahandler.Handler) *Reconciler {
	return &Reconciler{
		schemaHandler: ioHandler,
	}
}

// Reconcile processes schema generation for the given schema paths and cluster config
// Paths are treated as aliased cluster paths for the same cluster config.
func (r *Reconciler) Reconcile(ctx context.Context, schemaPaths []string, cfg *rest.Config, metadata *pmgatewayv1alpha1.ClusterMetadata) error {
	logger := log.FromContext(ctx)

	logger.Info("Processing schema generation", "paths", schemaPaths)

	// Create discovery client for the host cluster
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		logger.Error(err, "Failed to create discovery client")
		return fmt.Errorf("failed to create discovery client: %w", err)
	}

	// Create REST mapper for the host clusters
	restMapper, err := r.restMapperFromConfig(cfg)
	if err != nil {
		logger.Error(err, "Failed to create REST mapper")
		return fmt.Errorf("failed to create REST mapper: %w", err)
	}

	// We store both representation and schema files for each cluster paths.
	for _, schemaPath := range schemaPaths {
		logger.Info("Generating schema", "path", schemaPath)
		params := schemaGenerationParams{
			ClusterPath:     schemaPath,
			DiscoveryClient: discoveryClient,
			RESTMapper:      restMapper,
		}

		currentSchema, err := generateSchemaWithMetadata(ctx, params, metadata)
		if err != nil {
			logger.Error(err, "Failed to generate schema with metadata")
			return fmt.Errorf("failed to generate schema with metadata: %w", err)
		}

		// Read existing schema (if it exists)
		savedSchema, err := r.schemaHandler.Read(ctx, schemaPath)
		if err != nil && !errors.Is(err, schemahandler.ErrNotExist) {
			logger.Error(err, "Failed to read existing schema file")
			return fmt.Errorf("failed to read existing schema: %w", err)
		}

		// Write if file doesn't exist or content has changed
		if errors.Is(err, schemahandler.ErrNotExist) || !bytes.Equal(currentSchema, savedSchema) {
			if err := r.schemaHandler.Write(ctx, currentSchema, schemaPath); err != nil {
				logger.Error(err, "Failed to write schema", "path", schemaPath)
				return fmt.Errorf("failed to write schema: %w", err)
			}
			logger.Info("Schema file updated", "path", schemaPath)
		} else {
			logger.Info("Schema unchanged, skipping write", "path", schemaPath)
		}
	}

	return nil
}

func (r *Reconciler) Cleanup(ctx context.Context, schemaPaths []string) error {
	for _, schemaPath := range schemaPaths {
		err := r.schemaHandler.Delete(ctx, schemaPath)
		if err != nil && !errors.Is(err, schemahandler.ErrNotExist) {
			return fmt.Errorf("failed to delete schema for path %q: %w", schemaPath, err)
		}
	}
	return nil
}

// restMapperFromConfig creates a REST mapper from a config
func (r *Reconciler) restMapperFromConfig(cfg *rest.Config) (meta.RESTMapper, error) {
	httpClt, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, errors.Join(ErrCreateHTTPClient, err)
	}
	rm, err := apiutil.NewDynamicRESTMapper(cfg, httpClt)
	if err != nil {
		return nil, errors.Join(ErrCreateRESTMapper, err)
	}

	return rm, nil
}
