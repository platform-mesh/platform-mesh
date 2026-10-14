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

package contextual

import (
	"fmt"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	"go.platform-mesh.io/rebac-authz-webhook/pkg/clustercache"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/util"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// CheckInput contains preprocessed data for an OpenFGA check
type CheckInput struct {
	StoreID          string
	Object           string
	Relation         string
	User             string
	ContextualTuples []*openfgav1.TupleKey
}

// BuildCheckInput builds OpenFGA check parameters from resource attributes
// and extracts the contextual tuple building logic
func BuildCheckInput(
	attrs *authorizationv1.ResourceAttributes,
	user string,
	clusterName string,
	clusterInfo clustercache.ClusterInfo,
) (*CheckInput, error) {
	version := attrs.Version
	if version == "*" {
		// For some cluster level resources, the version may be set to "*".
		// Treat it as empty string to avoid issues with RESTMapper.
		version = ""
	}

	gvr := schema.GroupVersionResource{
		Group:    attrs.Group,
		Version:  version,
		Resource: attrs.Resource,
	}

	gvk, err := clusterInfo.RESTMapper.KindFor(gvr)
	if err != nil {
		return nil, fmt.Errorf("failed to get GVK for GVR %v: %w", gvr, err)
	}

	isNamespaced, err := apiutil.IsGVKNamespaced(gvk, clusterInfo.RESTMapper)
	if err != nil {
		return nil, fmt.Errorf("failed to determine if GVK %v is namespaced: %w", gvk, err)
	}

	singular, err := clusterInfo.RESTMapper.ResourceSingularizer(attrs.Resource)
	if err != nil {
		return nil, fmt.Errorf("failed to singularize resource %q: %w", attrs.Resource, err)
	}

	_, objectType := BuildObjectType(gvr, singular)

	object := renderObject(objectType, clusterName, attrs.Name)
	relation := attrs.Verb

	hasParent := util.ResolveOnParent(attrs.Verb)

	accountObject := fmt.Sprintf("core_platform-mesh_io_account:%s/%s", clusterInfo.ParentClusterID, clusterInfo.AccountName)

	if hasParent {
		relation = fmt.Sprintf("%s_%s", relation, strings.ReplaceAll(util.ResourceRelationName(gvr, maxRelationLength), ".", "_"))
		object = accountObject
	}

	var contextualTuples []*openfgav1.TupleKey
	if isNamespaced {
		namespaceObject := fmt.Sprintf("core_namespace:%s/%s", clusterName, attrs.Namespace)

		// parent the namespace to the account
		contextualTuples = append(contextualTuples, &openfgav1.TupleKey{
			Object:   namespaceObject,
			Relation: "parent",
			User:     accountObject,
		})

		if hasParent {
			object = namespaceObject
		} else {
			// parent the object to the namespace
			contextualTuples = append(contextualTuples, &openfgav1.TupleKey{
				Object:   renderObject(objectType, clusterName, attrs.Name),
				Relation: "parent",
				User:     namespaceObject,
			})
		}
	} else {
		contextualTuples = append(contextualTuples, &openfgav1.TupleKey{
			Object:   renderObject(objectType, clusterName, attrs.Name),
			Relation: "parent",
			User:     accountObject,
		})
	}

	checkInput := &CheckInput{
		StoreID:          clusterInfo.StoreID,
		Object:           object,
		Relation:         relation,
		User:             fmt.Sprintf("user:%s", user),
		ContextualTuples: contextualTuples,
	}

	return checkInput, nil
}

// renderObject builds the FGA object key for a resource. The resource name is
// encoded because OpenFGA requires an object to contain exactly one colon, and
// Kubernetes names validated as path segments (ClusterRoles, Roles, and their
// bindings) may legitimately contain colons themselves.
func renderObject(objectType, clusterName, name string) string {
	return fmt.Sprintf("%s:%s/%s", objectType, clusterName, util.EncodeName(name))
}

// BuildObjectType builds the FGA object type string from GVR and singular resource name.
// Returns the sanitized group name and the full object type.
func BuildObjectType(gvr schema.GroupVersionResource, singular string) (string, string) {
	group := util.CapGroupToRelationLength(gvr, maxRelationLength)
	group = strings.ReplaceAll(group, ".", "_")

	objectType := fmt.Sprintf("%s_%s", group, singular)
	// The model uses the capped group and the complete singular name.
	// The 50-character limit applies to relations, not object types.

	return group, objectType
}
