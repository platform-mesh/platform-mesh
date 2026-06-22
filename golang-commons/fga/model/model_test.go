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

import "testing"

func TestBuildObjectType(t *testing.T) {
	tests := []struct {
		name     string
		group    string
		singular string
		want     string
	}{
		{
			name:     "custom resource",
			group:    "core.platform-mesh.io",
			singular: "account",
			want:     "core_platform-mesh_io_account",
		},
		{
			name:     "core resource",
			group:    "",
			singular: "namespace",
			want:     "core_namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildObjectType(tt.group, tt.singular); got != tt.want {
				t.Fatalf("BuildObjectType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildObjectName(t *testing.T) {
	namespace := "ns1"

	tests := []struct {
		name      string
		group     string
		singular  string
		clusterID string
		resource  string
		namespace *string
		want      string
	}{
		{
			name:      "namespaced resource",
			group:     "core.platform-mesh.io",
			singular:  "component",
			clusterID: "cluster1",
			resource:  "comp1",
			namespace: &namespace,
			want:      "core_platform-mesh_io_component:cluster1/ns1/comp1",
		},
		{
			name:      "cluster scoped resource",
			group:     "core.platform-mesh.io",
			singular:  "account",
			clusterID: "cluster1",
			resource:  "acc1",
			want:      "core_platform-mesh_io_account:cluster1/acc1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildObjectName(tt.group, tt.singular, tt.clusterID, tt.resource, tt.namespace); got != tt.want {
				t.Fatalf("BuildObjectName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildParentTuples(t *testing.T) {
	namespaceObject := "core_namespace:cluster1/ns1"

	t.Run("namespaced", func(t *testing.T) {
		tuples := BuildParentTuples("core_platform-mesh_io_account:origin/account", "core_platform-mesh_io_component:cluster1/ns1/comp1", &namespaceObject)
		if len(tuples) != 2 {
			t.Fatalf("expected 2 tuples, got %d", len(tuples))
		}
		if tuples[0].Object != namespaceObject || tuples[0].User != "core_platform-mesh_io_account:origin/account" {
			t.Fatalf("unexpected first tuple: %+v", tuples[0])
		}
		if tuples[1].Object != "core_platform-mesh_io_component:cluster1/ns1/comp1" || tuples[1].User != namespaceObject {
			t.Fatalf("unexpected second tuple: %+v", tuples[1])
		}
	})

	t.Run("cluster scoped", func(t *testing.T) {
		tuples := BuildParentTuples("core_platform-mesh_io_account:origin/account", "core_platform-mesh_io_component:cluster1/comp1", nil)
		if len(tuples) != 1 {
			t.Fatalf("expected 1 tuple, got %d", len(tuples))
		}
		if tuples[0].Object != "core_platform-mesh_io_component:cluster1/comp1" || tuples[0].User != "core_platform-mesh_io_account:origin/account" {
			t.Fatalf("unexpected tuple: %+v", tuples[0])
		}
	})

	t.Run("self parent skipped", func(t *testing.T) {
		tuples := BuildParentTuples("core_platform-mesh_io_account:origin/account", "core_platform-mesh_io_account:origin/account", nil)
		if len(tuples) != 0 {
			t.Fatalf("expected 0 tuples, got %d", len(tuples))
		}
	})
}

func TestBuildContextualTuples(t *testing.T) {
	accountObject := "core_platform-mesh_io_account:origin-cluster/my-account"

	t.Run("namespaced resource produces two tuples", func(t *testing.T) {
		tuples, err := BuildContextualTuples(accountObject, ResourceContext{
			Group:     "apps",
			Kind:      "deployment",
			ClusterID: "tenant-cluster",
			Name:      "my-deploy",
			Namespace: "default",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(tuples) != 2 {
			t.Fatalf("expected 2 tuples, got %d", len(tuples))
		}
		// tuple[0]: namespace.parent = account
		wantNsObj := "core_namespace:tenant-cluster/default"
		if tuples[0].Object != wantNsObj {
			t.Errorf("tuple[0].Object = %q, want %q", tuples[0].Object, wantNsObj)
		}
		if tuples[0].User != accountObject {
			t.Errorf("tuple[0].User = %q, want %q", tuples[0].User, accountObject)
		}
		// tuple[1]: resource.parent = namespace
		wantResObj := "apps_deployment:tenant-cluster/default/my-deploy"
		if tuples[1].Object != wantResObj {
			t.Errorf("tuple[1].Object = %q, want %q", tuples[1].Object, wantResObj)
		}
		if tuples[1].User != wantNsObj {
			t.Errorf("tuple[1].User = %q, want %q", tuples[1].User, wantNsObj)
		}
	})

	t.Run("cluster-scoped resource produces one tuple", func(t *testing.T) {
		tuples, err := BuildContextualTuples(accountObject, ResourceContext{
			Group:     "core.platform-mesh.io",
			Kind:      "account",
			ClusterID: "child-cluster",
			Name:      "child-account",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(tuples) != 1 {
			t.Fatalf("expected 1 tuple, got %d", len(tuples))
		}
		wantObj := "core_platform-mesh_io_account:child-cluster/child-account"
		if tuples[0].Object != wantObj {
			t.Errorf("tuple[0].Object = %q, want %q", tuples[0].Object, wantObj)
		}
		if tuples[0].User != accountObject {
			t.Errorf("tuple[0].User = %q, want %q", tuples[0].User, accountObject)
		}
	})

	t.Run("self-referential account returns nil tuples", func(t *testing.T) {
		tuples, err := BuildContextualTuples(accountObject, ResourceContext{
			Group:     "core.platform-mesh.io",
			Kind:      "account",
			ClusterID: "origin-cluster",
			Name:      "my-account",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(tuples) != 0 {
			t.Fatalf("expected 0 tuples for self-referential account, got %d", len(tuples))
		}
	})

	t.Run("empty accountObject returns error", func(t *testing.T) {
		_, err := BuildContextualTuples("", ResourceContext{
			Group:     "apps",
			Kind:      "deployment",
			ClusterID: "cluster",
			Name:      "my-deploy",
		})
		if err == nil {
			t.Fatal("expected error for empty accountObject")
		}
	})
}
