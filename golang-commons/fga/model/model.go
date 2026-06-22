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

package model

import (
	"fmt"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	"go.platform-mesh.io/golang-commons/fga/util"
)

// BuildObjectType renders the canonical OpenFGA object type from an API group
// and singular resource name.
func BuildObjectType(group, singular string) string {
	return util.ConvertToTypeName(group, singular)
}

// BuildObjectName renders the canonical OpenFGA object name using the API group
// and singular resource name.
func BuildObjectName(group, singular, clusterID, name string, namespace *string) string {
	return BuildObjectNameFromType(BuildObjectType(group, singular), clusterID, name, namespace)
}

// BuildObjectNameFromType renders the canonical OpenFGA object name from a
// fully normalized OpenFGA object type.
func BuildObjectNameFromType(objectType, clusterID, name string, namespace *string) string {
	if namespace != nil && *namespace != "" {
		return fmt.Sprintf("%s:%s/%s/%s", objectType, clusterID, *namespace, name)
	}

	return fmt.Sprintf("%s:%s/%s", objectType, clusterID, name)
}

type ResourceContext struct {
	Group     string
	Kind      string
	ClusterID string
	Name      string
	Namespace string
}

// BuildContextualTuples produces the canonical Account → [Namespace →] Resource
// parent hierarchy as contextual tuples for an OpenFGA Check call.
//
// accountObject must be a fully-qualified FGA object name for the owning account,
// e.g. "core_platform-mesh_io_account:{originClusterID}/{accountName}".
//
// For namespaced resources two tuples are produced:
//
//	namespace.parent = accountObject
//	resource.parent  = namespaceObject
//
// For cluster-scoped resources one tuple is produced:
//
//	resource.parent = accountObject
//
// Returns (nil, nil) when object == accountObject (self-referential, e.g. an
// account resource that is its own FGA identity).
func BuildContextualTuples(accountObject string, res ResourceContext) ([]*openfgav1.TupleKey, error) {
	if accountObject == "" {
		return nil, fmt.Errorf("accountObject must not be empty")
	}

	var ns *string
	if res.Namespace != "" {
		ns = &res.Namespace
	}
	resourceObject := BuildObjectNameFromType(BuildObjectType(res.Group, res.Kind), res.ClusterID, res.Name, ns)

	var namespaceObject *string
	if res.Namespace != "" {
		nsObj := BuildObjectNameFromType(BuildObjectType("", "namespace"), res.ClusterID, res.Namespace, nil)
		namespaceObject = &nsObj
	}

	return BuildParentTuples(accountObject, resourceObject, namespaceObject), nil
}

// BuildParentTuples renders the canonical parent hierarchy tuples
func BuildParentTuples(parentObject, object string, namespaceObject *string) []*openfgav1.TupleKey {
	if namespaceObject != nil && *namespaceObject != "" {
		return []*openfgav1.TupleKey{
			{
				Object:   *namespaceObject,
				Relation: "parent",
				User:     parentObject,
			},
			{
				Object:   object,
				Relation: "parent",
				User:     *namespaceObject,
			},
		}
	}

	if object == parentObject {
		return nil
	}

	return []*openfgav1.TupleKey{
		{
			Object:   object,
			Relation: "parent",
			User:     parentObject,
		},
	}
}
