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
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlruntimefake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureValidatingWebhookCABundleRepairsStaleCAAndIsIdempotent(t *testing.T) {
	t.Parallel()

	configuration := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1",
		"kind":       "ValidatingWebhookConfiguration",
		"metadata": map[string]any{
			"name": accountWebhookConfigurationName,
		},
		"webhooks": []any{
			map[string]any{
				"name": "organization.validation.platform-mesh.ui",
				"clientConfig": map[string]any{
					"caBundle": base64.StdEncoding.EncodeToString([]byte("source-ca")),
					"url":      "https://account-operator-webhook.example.test/validate",
				},
			},
		},
	}}
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), configuration)
	destinationCA := []byte("destination-ca")

	changed, err := ensureValidatingWebhookCABundle(
		context.Background(), client, accountWebhookConfigurationName, destinationCA,
	)
	require.NoError(t, err)
	assert.True(t, changed)

	updated, err := client.Resource(validatingWebhookConfigurationGVR).Get(
		context.Background(), accountWebhookConfigurationName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	webhooks, found, err := unstructured.NestedSlice(updated.Object, "webhooks")
	require.NoError(t, err)
	require.True(t, found)
	clientConfig := webhooks[0].(map[string]any)["clientConfig"].(map[string]any)
	actual, _ := clientConfig["caBundle"].(string)
	assert.Equal(t, base64.StdEncoding.EncodeToString(destinationCA), actual)

	changed, err = ensureValidatingWebhookCABundle(
		context.Background(), client, accountWebhookConfigurationName, destinationCA,
	)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestEnsureOrganizationResourcesReadyTouchesRestoredResourcesOnce(t *testing.T) {
	t.Parallel()

	restore := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{UID: types.UID("restore-uid")}}
	readyAccount := testOrganizationResource(accountGVR, "Account", "sap", true)
	failedIDP := testOrganizationResource(identityProviderConfigurationGVR, "IdentityProviderConfiguration", "sap", false)
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), organizationResourcesListKinds(), readyAccount, failedIDP)

	ready, changed, err := ensureOrganizationResourcesReady(context.Background(), restore, client)
	require.NoError(t, err)
	assert.False(t, ready)
	assert.True(t, changed)

	account, err := client.Resource(accountGVR).Get(context.Background(), "sap", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "restore-uid", account.GetAnnotations()[organizationReconcileAnnotation])
	idp, err := client.Resource(identityProviderConfigurationGVR).Get(context.Background(), "sap", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "restore-uid", idp.GetAnnotations()[organizationReconcileAnnotation])

	ready, changed, err = ensureOrganizationResourcesReady(context.Background(), restore, client)
	require.NoError(t, err)
	assert.False(t, ready)
	assert.False(t, changed)

	require.NoError(t, unstructured.SetNestedSlice(idp.Object, readyConditions(true), "status", "conditions"))
	_, err = client.Resource(identityProviderConfigurationGVR).Update(context.Background(), idp, metav1.UpdateOptions{})
	require.NoError(t, err)

	ready, changed, err = ensureOrganizationResourcesReady(context.Background(), restore, client)
	require.NoError(t, err)
	assert.True(t, ready)
	assert.False(t, changed)
}

func TestEnsureOrganizationResourcesReadyRepairsRestoredWorkspaceTypeCollision(t *testing.T) {
	t.Parallel()

	restore := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{UID: types.UID("restore-uid")}}
	account := testOrganizationResource(accountGVR, "Account", "sap", false)
	require.NoError(t, unstructured.SetNestedSlice(account.Object, []any{
		map[string]any{
			"type":    "WorkspaceTypeSubroutine",
			"status":  "False",
			"message": "workspacetypes.tenancy.kcp.io \"sap-org\" already exists",
		},
	}, "status", "conditions"))
	orgType := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tenancy.kcp.io/v1alpha1",
		"kind":       "WorkspaceType",
		"metadata":   map[string]any{"name": "sap-org"},
	}}
	accountType := orgType.DeepCopy()
	accountType.SetName("sap-account")
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), organizationResourcesListKinds(), account, orgType, accountType)

	ready, changed, err := ensureOrganizationResourcesReady(context.Background(), restore, client)
	require.NoError(t, err)
	assert.False(t, ready)
	assert.True(t, changed)

	_, err = client.Resource(workspaceTypeGVR).Get(context.Background(), "sap-org", metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err))
	_, err = client.Resource(workspaceTypeGVR).Get(context.Background(), "sap-account", metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err))

	updated, err := client.Resource(accountGVR).Get(context.Background(), "sap", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "restore-uid", updated.GetAnnotations()[workspaceTypeCollisionRepairAnnotation])
}

func TestEnsureOrganizationControllersReadyRestartsBootstrapBeforeConsumers(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	bootstrap := readyDeployment(platformDeployment("security-operator"))
	accountOperator := readyDeployment(platformDeployment("account-operator"))
	systemOperator := readyDeployment(platformDeployment("security-operator-system"))
	client := ctrlruntimefake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&appsv1.Deployment{}).
		WithObjects(bootstrap, accountOperator, systemOperator).
		Build()
	recovery := NewPlatformRecoverySubroutine(client)
	restore := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{UID: types.UID("restore-uid")}}

	ready, err := recovery.ensureOrganizationControllersReady(context.Background(), restore)
	require.NoError(t, err)
	assert.False(t, ready)

	require.NoError(t, client.Get(context.Background(), ctrlruntimeclient.ObjectKeyFromObject(bootstrap), bootstrap))
	assert.Equal(t, "restore-uid", bootstrap.Spec.Template.Annotations[identityBootstrapRestartAnnotation])
	require.NoError(t, client.Get(context.Background(), ctrlruntimeclient.ObjectKeyFromObject(systemOperator), systemOperator))
	assert.Empty(t, systemOperator.Spec.Template.Annotations[organizationControllerRestartAnnotation])

	ready, err = recovery.ensureOrganizationControllersReady(context.Background(), restore)
	require.NoError(t, err)
	assert.False(t, ready)

	require.NoError(t, client.Get(context.Background(), ctrlruntimeclient.ObjectKeyFromObject(accountOperator), accountOperator))
	assert.Equal(t, "restore-uid", accountOperator.Spec.Template.Annotations[organizationControllerRestartAnnotation])
	require.NoError(t, client.Get(context.Background(), ctrlruntimeclient.ObjectKeyFromObject(systemOperator), systemOperator))
	assert.Equal(t, "restore-uid", systemOperator.Spec.Template.Annotations[organizationControllerRestartAnnotation])

	ready, err = recovery.ensureOrganizationControllersReady(context.Background(), restore)
	require.NoError(t, err)
	assert.True(t, ready)
}

func testOrganizationResource(
	gvr schema.GroupVersionResource,
	kind string,
	name string,
	ready bool,
) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gvr.Group + "/" + gvr.Version,
		"kind":       kind,
		"metadata": map[string]any{
			"name": name,
		},
		"status": map[string]any{
			"conditions": readyConditions(ready),
		},
	}}
}

func organizationResourcesListKinds() map[schema.GroupVersionResource]string {
	return map[schema.GroupVersionResource]string{
		accountGVR:                       "AccountList",
		identityProviderConfigurationGVR: "IdentityProviderConfigurationList",
		workspaceTypeGVR:                 "WorkspaceTypeList",
	}
}

func readyConditions(ready bool) []any {
	status := "False"
	if ready {
		status = "True"
	}
	return []any{map[string]any{
		"type":   "Ready",
		"status": status,
	}}
}

func readyDeployment(workload workloadRef) *appsv1.Deployment {
	deployment := testDeployment(workload, 1, nil)
	deployment.Status = appsv1.DeploymentStatus{
		ObservedGeneration: deployment.Generation,
		Replicas:           1,
		UpdatedReplicas:    1,
		ReadyReplicas:      1,
		AvailableReplicas:  1,
	}
	return deployment
}
