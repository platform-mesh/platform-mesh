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

package tuples

import (
	"testing"

	"github.com/stretchr/testify/assert"
	accountsv1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.platform-mesh.io/iam-service/pkg/graph"
)

func TestGenerateContextualTuples_WithNamespace(t *testing.T) {
	// Test case: Resource with namespace
	namespace := "test-namespace"
	rctx := &graph.ResourceContext{
		Group: "apps",
		Kind:  "Deployment",
		Resource: &graph.Resource{
			Name:      "test-deployment",
			Namespace: &namespace,
		},
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				Name:               "test-account",
				OriginClusterId:    "origin-cluster-123",
				GeneratedClusterId: "generated-cluster-456",
			},
		},
	}

	result := GenerateContextualTuples(rctx, ai)

	// Verify result structure
	assert.NotNil(t, result)
	assert.Equal(t, 2, len(result.TupleKeys))

	// Verify namespace tuple
	namespaceTuple := result.TupleKeys[0]
	assert.Equal(t, "core_namespace:generated-cluster-456/test-namespace", namespaceTuple.Object)
	assert.Equal(t, "parent", namespaceTuple.Relation)
	assert.Equal(t, "core_platform-mesh_io_account:origin-cluster-123/test-account", namespaceTuple.User)

	// Verify resource tuple
	resourceTuple := result.TupleKeys[1]
	assert.Equal(t, "apps_deployment:generated-cluster-456/test-namespace/test-deployment", resourceTuple.Object)
	assert.Equal(t, "parent", resourceTuple.Relation)
	assert.Equal(t, "core_namespace:generated-cluster-456/test-namespace", resourceTuple.User)
}

func TestGenerateContextualTuples_WithoutNamespace(t *testing.T) {
	// Test case: Resource without namespace (cluster-scoped)
	rctx := &graph.ResourceContext{
		Group: "rbac.authorization.k8s.io",
		Kind:  "ClusterRole",
		Resource: &graph.Resource{
			Name:      "test-cluster-role",
			Namespace: nil,
		},
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				Name:               "test-account",
				OriginClusterId:    "origin-cluster-123",
				GeneratedClusterId: "generated-cluster-456",
			},
		},
	}

	result := GenerateContextualTuples(rctx, ai)

	// Verify result structure
	assert.NotNil(t, result)
	assert.Equal(t, 1, len(result.TupleKeys))

	// Verify resource tuple (no namespace tuple should be created)
	resourceTuple := result.TupleKeys[0]
	assert.Equal(t, "rbac_authorization_k8s_io_clusterrole:generated-cluster-456/test-cluster-role", resourceTuple.Object)
	assert.Equal(t, "parent", resourceTuple.Relation)
	assert.Equal(t, "core_platform-mesh_io_account:origin-cluster-123/test-account", resourceTuple.User)
}

func TestGenerateContextualTuples_ManagedTuple_Account(t *testing.T) {
	// Test case: Managed tuple (Account) should not create resource tuple
	namespace := "test-namespace"
	rctx := &graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: &namespace,
		},
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				Name:               "test-account",
				OriginClusterId:    "origin-cluster-123",
				GeneratedClusterId: "generated-cluster-456",
			},
		},
	}

	result := GenerateContextualTuples(rctx, ai)

	// Verify result structure
	assert.NotNil(t, result)
	assert.Equal(t, 1, len(result.TupleKeys)) // Only namespace tuple, no resource tuple

	// Verify only namespace tuple exists
	namespaceTuple := result.TupleKeys[0]
	assert.Equal(t, "core_namespace:generated-cluster-456/test-namespace", namespaceTuple.Object)
	assert.Equal(t, "parent", namespaceTuple.Relation)
	assert.Equal(t, "core_platform-mesh_io_account:origin-cluster-123/test-account", namespaceTuple.User)
}

func TestGenerateContextualTuples_ManagedTuple_WithoutNamespace(t *testing.T) {
	// Test case: Managed tuple (Account) without namespace should create no tuples
	rctx := &graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: nil,
		},
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				Name:               "test-account",
				OriginClusterId:    "origin-cluster-123",
				GeneratedClusterId: "generated-cluster-456",
			},
		},
	}

	result := GenerateContextualTuples(rctx, ai)

	// Verify result structure
	assert.NotNil(t, result)
	assert.Equal(t, 0, len(result.TupleKeys)) // No tuples should be created
}

func TestGenerateContextualTuples_EmptyGroup(t *testing.T) {
	// Test case: Resource with empty group
	namespace := "test-namespace"
	rctx := &graph.ResourceContext{
		Group: "",
		Kind:  "Pod",
		Resource: &graph.Resource{
			Name:      "test-pod",
			Namespace: &namespace,
		},
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				Name:               "test-account",
				OriginClusterId:    "origin-cluster-123",
				GeneratedClusterId: "generated-cluster-456",
			},
		},
	}

	result := GenerateContextualTuples(rctx, ai)

	// Verify result structure
	assert.NotNil(t, result)
	assert.Equal(t, 2, len(result.TupleKeys))

	// Verify namespace tuple
	namespaceTuple := result.TupleKeys[0]
	assert.Equal(t, "core_namespace:generated-cluster-456/test-namespace", namespaceTuple.Object)
	assert.Equal(t, "parent", namespaceTuple.Relation)
	assert.Equal(t, "core_platform-mesh_io_account:origin-cluster-123/test-account", namespaceTuple.User)

	// Verify resource tuple - empty group should be handled correctly
	resourceTuple := result.TupleKeys[1]
	assert.Equal(t, "core_pod:generated-cluster-456/test-namespace/test-pod", resourceTuple.Object)
	assert.Equal(t, "parent", resourceTuple.Relation)
	assert.Equal(t, "core_namespace:generated-cluster-456/test-namespace", resourceTuple.User)
}

func TestManagedTuple_CorePlatformMeshAccount(t *testing.T) {
	// Test case: core.platform-mesh.io Account should be managed
	result := managedTuple("core.platform-mesh.io", "Account")
	assert.True(t, result)
}

func TestManagedTuple_CorePlatformMeshAccountCaseInsensitive(t *testing.T) {
	// Test case: Case insensitive matching
	result := managedTuple("CORE.PLATFORM-MESH.IO", "ACCOUNT")
	assert.True(t, result)

	result = managedTuple("Core.Platform-Mesh.Io", "account")
	assert.True(t, result)
}

func TestManagedTuple_CorePlatformMeshOtherKind(t *testing.T) {
	// Test case: core.platform-mesh.io with other kinds should not be managed
	result := managedTuple("core.platform-mesh.io", "User")
	assert.False(t, result)

	result = managedTuple("core.platform-mesh.io", "Role")
	assert.False(t, result)
}

func TestManagedTuple_OtherGroup(t *testing.T) {
	// Test case: Other groups should not be managed
	result := managedTuple("apps", "Deployment")
	assert.False(t, result)

	result = managedTuple("rbac.authorization.k8s.io", "ClusterRole")
	assert.False(t, result)

	result = managedTuple("", "Pod")
	assert.False(t, result)
}

func TestManagedTuple_EmptyInputs(t *testing.T) {
	// Test case: Empty inputs should not be managed
	result := managedTuple("", "")
	assert.False(t, result)

	result = managedTuple("core.platform-mesh.io", "")
	assert.False(t, result)

	result = managedTuple("", "Account")
	assert.False(t, result)
}

func TestGenerateContextualTuples_ComplexGroupName(t *testing.T) {
	// Test case: Complex group name conversion
	namespace := "test-namespace"
	rctx := &graph.ResourceContext{
		Group: "networking.istio.io",
		Kind:  "VirtualService",
		Resource: &graph.Resource{
			Name:      "test-virtual-service",
			Namespace: &namespace,
		},
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				Name:               "test-account",
				OriginClusterId:    "origin-cluster-123",
				GeneratedClusterId: "generated-cluster-456",
			},
		},
	}

	result := GenerateContextualTuples(rctx, ai)

	// Verify result structure
	assert.NotNil(t, result)
	assert.Equal(t, 2, len(result.TupleKeys))

	// Verify resource tuple with complex group name
	resourceTuple := result.TupleKeys[1]
	assert.Equal(t, "networking_istio_io_virtualservice:generated-cluster-456/test-namespace/test-virtual-service", resourceTuple.Object)
	assert.Equal(t, "parent", resourceTuple.Relation)
	assert.Equal(t, "core_namespace:generated-cluster-456/test-namespace", resourceTuple.User)
}

func TestGenerateContextualTuples_SpecialCharactersInNames(t *testing.T) {
	// Test case: Special characters in resource names
	namespace := "test-namespace-with-hyphens"
	rctx := &graph.ResourceContext{
		Group: "batch",
		Kind:  "Job",
		Resource: &graph.Resource{
			Name:      "test-job-with-hyphens",
			Namespace: &namespace,
		},
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account-with-hyphens"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				Name:               "test-account-with-hyphens",
				OriginClusterId:    "origin-cluster-with-hyphens",
				GeneratedClusterId: "generated-cluster-with-hyphens",
			},
		},
	}

	result := GenerateContextualTuples(rctx, ai)

	// Verify result structure
	assert.NotNil(t, result)
	assert.Equal(t, 2, len(result.TupleKeys))

	// Verify namespace tuple
	namespaceTuple := result.TupleKeys[0]
	assert.Equal(t, "core_namespace:generated-cluster-with-hyphens/test-namespace-with-hyphens", namespaceTuple.Object)
	assert.Equal(t, "parent", namespaceTuple.Relation)
	assert.Equal(t, "core_platform-mesh_io_account:origin-cluster-with-hyphens/test-account-with-hyphens", namespaceTuple.User)

	// Verify resource tuple
	resourceTuple := result.TupleKeys[1]
	assert.Equal(t, "batch_job:generated-cluster-with-hyphens/test-namespace-with-hyphens/test-job-with-hyphens", resourceTuple.Object)
	assert.Equal(t, "parent", resourceTuple.Relation)
	assert.Equal(t, "core_namespace:generated-cluster-with-hyphens/test-namespace-with-hyphens", resourceTuple.User)
}
