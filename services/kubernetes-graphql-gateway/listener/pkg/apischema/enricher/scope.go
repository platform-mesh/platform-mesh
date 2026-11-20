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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Scope adds x-kubernetes-scope extension to schemas.
// It determines whether each resource is namespace-scoped or cluster-scoped.
//
// Scope is NOT present in the OpenAPI document, so it must be resolved from
// discovery. Two independent sources are used, in order:
//  1. The server's preferred-resources discovery list (APIResource.Namespaced).
//     This is the authoritative source and is already fetched by the caller.
//  2. A RESTMapper lookup, as a fallback for GVKs absent from the preferred
//     list (e.g. non-preferred versions, since ServerPreferredResources only
//     returns the preferred version of each group).
//
// If neither source can resolve scope for a schema that carries a GVK, the
// resource would be silently dropped by the gateway (which requires the scope
// extension). To avoid that being invisible, such cases are logged loudly.
type Scope struct {
	mapper     meta.RESTMapper
	namespaced map[schema.GroupVersionKind]bool
}

// NewScope creates a new Scope enricher.
//
// resources is the discovery list (as returned by ServerPreferredResources)
// used as the primary scope source; mapper is used as a fallback. Either may
// be nil, but providing both maximizes resilience.
func NewScope(mapper meta.RESTMapper, resources []*metav1.APIResourceList) *Scope {
	return &Scope{
		mapper:     mapper,
		namespaced: buildNamespacedIndex(resources),
	}
}

// buildNamespacedIndex builds a GVK -> namespaced lookup from a discovery list.
func buildNamespacedIndex(resources []*metav1.APIResourceList) map[schema.GroupVersionKind]bool {
	index := make(map[schema.GroupVersionKind]bool)
	for _, apiResList := range resources {
		if apiResList == nil {
			continue
		}
		gv, err := schema.ParseGroupVersion(apiResList.GroupVersion)
		if err != nil {
			continue
		}
		for _, res := range apiResList.APIResources {
			// Skip subresources (e.g. "pods/status"); they are not top-level kinds.
			if res.Kind == "" {
				continue
			}
			index[schema.GroupVersionKind{
				Group:   gv.Group,
				Version: gv.Version,
				Kind:    res.Kind,
			}] = res.Namespaced
		}
	}
	return index
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

		namespaced, ok := e.resolveScope(*entry.GVK)
		if !ok {
			// Neither discovery nor the RESTMapper could determine scope.
			// The gateway requires the scope extension, so this resource would
			// be dropped from the generated schema. Log loudly so a vanished
			// API group is never silent.
			logger.Error(nil, "unable to determine resource scope; resource will be omitted from the generated schema",
				"gvk", entry.GVK.String())
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

// resolveScope determines whether a GVK is namespace-scoped, preferring the
// discovery list and falling back to the RESTMapper. The bool return is false
// only when neither source can resolve the scope.
func (e *Scope) resolveScope(gvk schema.GroupVersionKind) (namespaced bool, ok bool) {
	if ns, found := e.namespaced[gvk]; found {
		return ns, true
	}

	if e.mapper == nil {
		return false, false
	}

	ns, err := apiutil.IsGVKNamespaced(gvk, e.mapper)
	if err != nil {
		return false, false
	}
	return ns, true
}
