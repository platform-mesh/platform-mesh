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

import "testing"

func TestObjectBuildersEncodeResourceName(t *testing.T) {
	const (
		resourceType = "rbac_authorization_k8s_io_clusterrole"
		clusterID    = "cluster-123"
		resourceName = "system:controller#with space"
		role         = "owner"
	)

	if got, want := buildRoleObjectID(resourceType, clusterID, resourceName, role), "rbac_authorization_k8s_io_clusterrole/cluster-123/system%3Acontroller%23with%20space/owner"; got != want {
		t.Fatalf("buildRoleObjectID() = %q, want %q", got, want)
	}
	if got, want := buildRoleObject(resourceType, clusterID, resourceName, role), "role:rbac_authorization_k8s_io_clusterrole/cluster-123/system%3Acontroller%23with%20space/owner"; got != want {
		t.Fatalf("buildRoleObject() = %q, want %q", got, want)
	}
	if got, want := buildRoleUserset(resourceType, clusterID, resourceName, role), "role:rbac_authorization_k8s_io_clusterrole/cluster-123/system%3Acontroller%23with%20space/owner#assignee"; got != want {
		t.Fatalf("buildRoleUserset() = %q, want %q", got, want)
	}
	if got, want := buildResourceObject(resourceType, clusterID, resourceName, nil), "rbac_authorization_k8s_io_clusterrole:cluster-123/system%3Acontroller%23with%20space"; got != want {
		t.Fatalf("buildResourceObject() = %q, want %q", got, want)
	}
}
