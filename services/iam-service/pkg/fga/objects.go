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

package fga

import (
	"fmt"

	fgamodel "go.platform-mesh.io/golang-commons/fga/model"
	fgautil "go.platform-mesh.io/golang-commons/fga/util"
)

func buildRoleObjectID(resourceType, clusterID, resourceName, role string) string {
	return fmt.Sprintf("%s/%s/%s/%s", resourceType, clusterID, fgautil.EncodeObjectIDPart(resourceName), role)
}

func buildRoleObject(resourceType, clusterID, resourceName, role string) string {
	return "role:" + buildRoleObjectID(resourceType, clusterID, resourceName, role)
}

func buildRoleUserset(resourceType, clusterID, resourceName, role string) string {
	return buildRoleObject(resourceType, clusterID, resourceName, role) + "#assignee"
}

func buildResourceObject(resourceType, clusterID, resourceName string, namespace *string) string {
	return fgamodel.BuildObjectNameFromType(resourceType, clusterID, resourceName, namespace)
}
