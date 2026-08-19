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

package restore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlruntimefake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureIdentityProviderAPIBindingResourcesRepairsCoreNamingConflict(t *testing.T) {
	t.Parallel()

	identityResource := map[string]any{
		"group":   identityProviderResourceGroup,
		"name":    identityProviderResourceName,
		"schema":  "v1.identityproviderconfigurations.core.platform-mesh.io",
		"storage": map[string]any{"crd": map[string]any{}},
	}
	accountResource := map[string]any{
		"group":   "core.platform-mesh.io",
		"name":    "accounts",
		"schema":  "v1.accounts.core.platform-mesh.io",
		"storage": map[string]any{"crd": map[string]any{}},
	}

	systemBinding := testAPIBinding(systemAPIBindingName, nil, []any{map[string]any{
		"group":    identityProviderResourceGroup,
		"resource": identityProviderResourceName,
	}})
	coreBinding := testAPIBinding(coreAPIBindingName, map[string]string{
		staleCoreBindingResetAnnotation: "restore-uid",
	}, nil)
	systemExport := testAPIExport(systemAPIBindingName, []any{identityResource})
	coreExport := testAPIExport(coreAPIBindingName, []any{accountResource, identityResource})

	client := dynamicfake.NewSimpleDynamicClient(
		runtime.NewScheme(),
		systemBinding,
		coreBinding,
		systemExport,
		coreExport,
	)
	recovery := &PlatformRecoverySubroutine{}
	restore := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{UID: types.UID("restore-uid")}}

	ready, err := recovery.ensureIdentityProviderAPIBindingResources(
		context.Background(), restore, client, client, client,
	)
	require.NoError(t, err)
	assert.False(t, ready)

	updatedExport, err := client.Resource(apiExportGVR).Get(
		context.Background(), coreAPIBindingName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	resources, found, err := unstructured.NestedSlice(updatedExport.Object, "spec", "resources")
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, containsAPIExportResource(resources, identityProviderResourceGroup, "accounts"))
	assert.False(t, containsAPIExportResource(resources, identityProviderResourceGroup, identityProviderResourceName))

	ready, err = recovery.ensureIdentityProviderAPIBindingResources(
		context.Background(), restore, client, client, client,
	)
	require.NoError(t, err)
	assert.False(t, ready)

	updatedBinding, err := client.Resource(apiBindingGVR).Get(
		context.Background(), coreAPIBindingName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.NotContains(t, updatedBinding.GetAnnotations(), staleCoreBindingResetAnnotation)

	ready, err = recovery.ensureIdentityProviderAPIBindingResources(
		context.Background(), restore, client, client, client,
	)
	require.NoError(t, err)
	assert.True(t, ready)
}

func TestEnsureKCPVirtualWorkspaceSchemaReaderIncludesWorkspaceAccess(t *testing.T) {
	t.Parallel()

	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())

	changed, err := ensureKCPVirtualWorkspaceSchemaReader(context.Background(), client)
	require.NoError(t, err)
	assert.True(t, changed)

	role, err := client.Resource(kcpClusterRoleGVR).Get(
		context.Background(), "platform-restore-virtual-workspace-schema-reader", metav1.GetOptions{},
	)
	require.NoError(t, err)
	rules, found, err := unstructured.NestedSlice(role.Object, "rules")
	require.NoError(t, err)
	require.True(t, found)

	assert.True(t, containsNonResourceRule(rules, "/", "access"))
	assert.True(t, containsNonResourceRule(rules, "/apis", "get"))
}

func TestEnsureKCPWebhookBootstrapRestoredRepairsIncompleteTransition(t *testing.T) {
	t.Parallel()

	const webhookArg = "--authorization-webhook-config-file=/etc/kcp/authorization/webhook/kubeconfig"
	restore := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{
		Name: "restore", UID: types.UID("restore-uid"),
	}}
	state := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: restoreStateConfigMapName(restore), Namespace: platformMeshNamespace,
		},
		Data: map[string]string{
			"platformRestoreUID":           "restore-uid",
			kcpWebhookBootstrapDisabledKey: "restore-uid",
			replicaStateKey(workloadRef{Namespace: kcpOperatorNamespace, Kind: "Deployment", Name: "kcp-operator"}): "2",
		},
	}

	objects := make([]ctrlruntimeclient.Object, 1, 1+len(kcpWebhookConsumers)+1)
	objects[0] = state
	for _, workload := range kcpWebhookConsumers {
		state.Data[kcpWebhookArgumentPrefix+workload.Name] = webhookArg
		objects = append(objects, testDeployment(workload, 1, []string{"--authorization-webhook-version=v1"}))
	}
	operator := workloadRef{Namespace: kcpOperatorNamespace, Kind: "Deployment", Name: "kcp-operator"}
	objects = append(objects, testDeployment(operator, 0, nil))

	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	client := ctrlruntimefake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&appsv1.Deployment{}).
		WithObjects(objects...).
		Build()
	recovery := NewPlatformRecoverySubroutine(client)

	ready, err := recovery.ensureKCPWebhookBootstrapRestored(context.Background(), restore)
	require.NoError(t, err)
	assert.False(t, ready)

	require.NoError(t, client.Get(context.Background(), ctrlruntimeclient.ObjectKeyFromObject(state), state))
	assert.Equal(t, "restore-uid", state.Data[kcpWebhookBootstrapRestoredKey])
	for _, workload := range kcpWebhookConsumers {
		deployment := &appsv1.Deployment{}
		require.NoError(t, client.Get(context.Background(), ctrlruntimeclient.ObjectKey{
			Namespace: workload.Namespace, Name: workload.Name,
		}, deployment))
		assert.Equal(t, 1, countString(deployment.Spec.Template.Spec.Containers[0].Args, webhookArg))
	}

	operatorDeployment := &appsv1.Deployment{}
	require.NoError(t, client.Get(context.Background(), ctrlruntimeclient.ObjectKey{
		Namespace: operator.Namespace, Name: operator.Name,
	}, operatorDeployment))
	require.NotNil(t, operatorDeployment.Spec.Replicas)
	assert.Equal(t, int32(2), *operatorDeployment.Spec.Replicas)

	// The restoration marker alone must not report success while the operator
	// rollout is still unavailable.
	ready, err = recovery.ensureKCPWebhookBootstrapRestored(context.Background(), restore)
	require.NoError(t, err)
	assert.False(t, ready)

	operatorDeployment.Status.ObservedGeneration = operatorDeployment.Generation
	operatorDeployment.Status.Replicas = 2
	operatorDeployment.Status.UpdatedReplicas = 2
	operatorDeployment.Status.ReadyReplicas = 2
	operatorDeployment.Status.AvailableReplicas = 2
	require.NoError(t, client.Status().Update(context.Background(), operatorDeployment))

	ready, err = recovery.ensureKCPWebhookBootstrapRestored(context.Background(), restore)
	require.NoError(t, err)
	assert.True(t, ready)
}

func TestDeploymentReadyWaitsForUpdatedRolloutReplica(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	deployment := testDeployment(platformDeployment("rolling"), 1, nil)
	deployment.Generation = 2
	deployment.Status = appsv1.DeploymentStatus{
		ObservedGeneration:  2,
		Replicas:            2, // one updated pod and one old ready pod
		UpdatedReplicas:     1,
		ReadyReplicas:       1,
		AvailableReplicas:   1,
		UnavailableReplicas: 1,
	}
	client := ctrlruntimefake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&appsv1.Deployment{}).
		WithObjects(deployment).
		Build()

	ready, err := platformDeployment(deployment.Name).ready(context.Background(), client)
	require.NoError(t, err)
	assert.False(t, ready)

	deployment.Status.Replicas = 1
	deployment.Status.UnavailableReplicas = 0
	require.NoError(t, client.Status().Update(context.Background(), deployment))
	ready, err = platformDeployment(deployment.Name).ready(context.Background(), client)
	require.NoError(t, err)
	assert.True(t, ready)
}

func TestEnsureKCPFrontProxyReadyRestartsOnceAfterKCPRecovery(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	frontProxy := testDeployment(platformDeployment("frontproxy-front-proxy"), 1, nil)
	frontProxy.Status = appsv1.DeploymentStatus{
		ObservedGeneration: frontProxy.Generation,
		Replicas:           1,
		UpdatedReplicas:    1,
		ReadyReplicas:      1,
		AvailableReplicas:  1,
	}
	client := ctrlruntimefake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&appsv1.Deployment{}).
		WithObjects(frontProxy).
		Build()
	recovery := NewPlatformRecoverySubroutine(client)
	restore := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{UID: types.UID("restore-uid")}}

	ready, err := recovery.ensureKCPFrontProxyReady(context.Background(), restore)
	require.NoError(t, err)
	assert.False(t, ready)

	require.NoError(t, client.Get(context.Background(), ctrlruntimeclient.ObjectKeyFromObject(frontProxy), frontProxy))
	assert.Equal(t, "restore-uid", frontProxy.Spec.Template.Annotations[frontProxyRestartAnnotation])

	ready, err = recovery.ensureKCPFrontProxyReady(context.Background(), restore)
	require.NoError(t, err)
	assert.True(t, ready)
}

func testDeployment(workload workloadRef, replicas int32, args []string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: workload.Name, Namespace: workload.Namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(replicas),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "main", Args: args,
			}}}},
		},
	}
}

func countString(items []string, value string) int {
	count := 0
	for _, item := range items {
		if item == value {
			count++
		}
	}
	return count
}

func containsNonResourceRule(rules []any, path, verb string) bool {
	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		paths, _, _ := unstructured.NestedStringSlice(rule, "nonResourceURLs")
		verbs, _, _ := unstructured.NestedStringSlice(rule, "verbs")
		if containsString(paths, path) && containsString(verbs, verb) {
			return true
		}
	}
	return false
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func testAPIBinding(name string, annotations map[string]string, boundResources []any) *unstructured.Unstructured {
	binding := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apis.kcp.io/v1alpha2",
		"kind":       "APIBinding",
		"metadata": map[string]any{
			"name": name,
		},
		"spec": map[string]any{
			"reference": map[string]any{
				"export": map[string]any{
					"name": name,
					"path": platformSystemPath,
				},
			},
		},
	}}
	binding.SetAnnotations(annotations)
	if boundResources != nil {
		binding.Object["status"] = map[string]any{"boundResources": boundResources}
	}
	return binding
}

func testAPIExport(name string, resources []any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apis.kcp.io/v1alpha2",
		"kind":       "APIExport",
		"metadata": map[string]any{
			"name": name,
		},
		"spec": map[string]any{"resources": resources},
	}}
}
