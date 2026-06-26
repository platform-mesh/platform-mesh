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

package enricher

import (
	"context"

	pmgateway "go.platform-mesh.io/apis/gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/apischema"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Scope adds x-kubernetes-scope extension to schemas.
// It determines whether each resource is namespace-scoped or cluster-scoped.
type Scope struct {
	mapper meta.RESTMapper
}

// NewScope creates a new Scope enricher.
func NewScope(mapper meta.RESTMapper) *Scope {
	return &Scope{mapper: mapper}
}

// Name returns the enricher name for logging.
func (e *Scope) Name() string {
	return "scope"
}

// Enrich adds scope information to all schemas with GVK.
func (e *Scope) Enrich(ctx context.Context, schemas *apischema.SchemaSet) error {
	logger := log.FromContext(ctx)

	for _, entry := range schemas.All() {
		if entry.GVK == nil {
			continue
		}

		namespaced, err := apiutil.IsGVKNamespaced(*entry.GVK, e.mapper)
		if err != nil {
			logger.V(4).WithValues(
				"gvk", entry.GVK,
				"error", err,
			).Info("failed to determine scope")
			continue
		}

		if namespaced {
			entry.Schema.AddExtension(pmgateway.ScopeExtensionKey, apiextensionsv1.NamespaceScoped)
		} else {
			entry.Schema.AddExtension(pmgateway.ScopeExtensionKey, apiextensionsv1.ClusterScoped)
		}
	}

	return nil
}
