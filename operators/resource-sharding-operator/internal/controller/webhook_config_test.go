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

//nolint:goconst
package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/apis/sharding/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// newWebhookConfigScheme builds a scheme that includes admissionregistration types.
func newWebhookConfigScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, v1alpha1.AddToScheme(s))
	return s
}

// buildResourceSharding returns a minimal ResourceSharding used in webhook_config tests.
func buildResourceSharding(name string, webhookEnabled bool) *v1alpha1.ResourceSharding {
	return &v1alpha1.ResourceSharding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID("uid-" + name),
		},
		Spec: v1alpha1.ResourceShardingSpec{
			Target: v1alpha1.TargetResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
			},
			Shards:  []v1alpha1.ShardRef{{Name: "shard-a"}},
			Webhook: v1alpha1.WebhookConfig{Enabled: webhookEnabled},
		},
	}
}

// ---------------------------------------------------------------------------
// EnsureWebhookConfiguration — create path
// ---------------------------------------------------------------------------

func TestEnsureWebhookConfiguration_CreatesWhenNotExists(t *testing.T) {
	scheme := newWebhookConfigScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	rs := buildResourceSharding("my-rs", true)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	err := EnsureWebhookConfiguration(context.Background(), fc, rs, gvr, "test-ns", "my-service")
	require.NoError(t, err)

	// Verify the object was created
	created := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err = fc.Get(context.Background(), types.NamespacedName{Name: "resource-sharding-my-rs"}, created)
	require.NoError(t, err, "MutatingWebhookConfiguration should have been created")
}

func TestEnsureWebhookConfiguration_WebhookNameIsFixed(t *testing.T) {
	scheme := newWebhookConfigScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	rs := buildResourceSharding("my-rs", true)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	require.NoError(t, EnsureWebhookConfiguration(context.Background(), fc, rs, gvr, "test-ns", "my-service"))

	created := &admissionregistrationv1.MutatingWebhookConfiguration{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "resource-sharding-my-rs"}, created))

	require.Len(t, created.Webhooks, 1)
	// Service path must be the fixed /mutate-shard-assign, not /mutate-<cr-name>
	require.NotNil(t, created.Webhooks[0].ClientConfig.Service)
	assert.Equal(t, "/mutate-shard-assign", *created.Webhooks[0].ClientConfig.Service.Path)
}

func TestEnsureWebhookConfiguration_SetsServiceNameAndNamespace(t *testing.T) {
	scheme := newWebhookConfigScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	rs := buildResourceSharding("my-rs", true)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	require.NoError(t, EnsureWebhookConfiguration(context.Background(), fc, rs, gvr, "my-namespace", "my-svc"))

	created := &admissionregistrationv1.MutatingWebhookConfiguration{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "resource-sharding-my-rs"}, created))

	svc := created.Webhooks[0].ClientConfig.Service
	require.NotNil(t, svc)
	assert.Equal(t, "my-svc", svc.Name)
	assert.Equal(t, "my-namespace", svc.Namespace)
}

func TestEnsureWebhookConfiguration_SetsOwnerReference(t *testing.T) {
	scheme := newWebhookConfigScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	rs := buildResourceSharding("my-rs", true)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	require.NoError(t, EnsureWebhookConfiguration(context.Background(), fc, rs, gvr, "ns", "svc"))

	created := &admissionregistrationv1.MutatingWebhookConfiguration{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "resource-sharding-my-rs"}, created))

	require.Len(t, created.OwnerReferences, 1)
	ref := created.OwnerReferences[0]
	assert.Equal(t, "ResourceSharding", ref.Kind)
	assert.Equal(t, "my-rs", ref.Name)
	assert.Equal(t, rs.UID, ref.UID)
	require.NotNil(t, ref.Controller)
	assert.True(t, *ref.Controller)
}

func TestEnsureWebhookConfiguration_SetsGVRRules(t *testing.T) {
	scheme := newWebhookConfigScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	rs := buildResourceSharding("my-rs", true)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	require.NoError(t, EnsureWebhookConfiguration(context.Background(), fc, rs, gvr, "ns", "svc"))

	created := &admissionregistrationv1.MutatingWebhookConfiguration{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "resource-sharding-my-rs"}, created))

	require.Len(t, created.Webhooks[0].Rules, 1)
	rule := created.Webhooks[0].Rules[0].Rule
	assert.Equal(t, []string{"apps"}, rule.APIGroups)
	assert.Equal(t, []string{"v1"}, rule.APIVersions)
	assert.Equal(t, []string{"deployments"}, rule.Resources)
}

// ---------------------------------------------------------------------------
// EnsureWebhookConfiguration — update path
// ---------------------------------------------------------------------------

func TestEnsureWebhookConfiguration_UpdatesExisting(t *testing.T) {
	scheme := newWebhookConfigScheme(t)

	// Pre-populate with an empty (stale) webhook config — no webhooks yet.
	oldWebhook := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "resource-sharding-my-rs",
		},
	}

	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(oldWebhook).
		Build()

	rs := buildResourceSharding("my-rs", true)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	err := EnsureWebhookConfiguration(context.Background(), fc, rs, gvr, "ns", "svc")
	require.NoError(t, err)

	updated := &admissionregistrationv1.MutatingWebhookConfiguration{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "resource-sharding-my-rs"}, updated))

	assert.Len(t, updated.Webhooks, 1, "existing config should have been updated with webhook entries")
	assert.Len(t, updated.OwnerReferences, 1, "owner references should be set on update")
}

func TestEnsureWebhookConfiguration_UpdatesWebhooksPreservesPath(t *testing.T) {
	scheme := newWebhookConfigScheme(t)

	oldPath := "/old-path"
	oldWebhook := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "resource-sharding-update-rs",
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name: "old.webhook.name",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Name: "old-svc",
						Path: &oldPath,
					},
				},
				AdmissionReviewVersions: []string{"v1"},
			},
		},
	}

	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(oldWebhook).
		Build()

	rs := buildResourceSharding("update-rs", true)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

	require.NoError(t, EnsureWebhookConfiguration(context.Background(), fc, rs, gvr, "ns", "new-svc"))

	updated := &admissionregistrationv1.MutatingWebhookConfiguration{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "resource-sharding-update-rs"}, updated))

	require.Len(t, updated.Webhooks, 1)
	// Path should have been replaced with the fixed /mutate-shard-assign
	assert.Equal(t, "/mutate-shard-assign", *updated.Webhooks[0].ClientConfig.Service.Path)
	assert.Equal(t, "new-svc", updated.Webhooks[0].ClientConfig.Service.Name)
}

// ---------------------------------------------------------------------------
// EnsureWebhookConfiguration — webhook disabled path (delegates to Delete)
// ---------------------------------------------------------------------------

func TestEnsureWebhookConfiguration_DisabledDeletesExisting(t *testing.T) {
	scheme := newWebhookConfigScheme(t)

	existing := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "resource-sharding-disabled-rs",
		},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	rs := buildResourceSharding("disabled-rs", false) // webhook disabled
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	err := EnsureWebhookConfiguration(context.Background(), fc, rs, gvr, "ns", "svc")
	require.NoError(t, err)

	// The MutatingWebhookConfiguration should have been deleted
	deleted := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err = fc.Get(context.Background(), types.NamespacedName{Name: "resource-sharding-disabled-rs"}, deleted)
	assert.True(t, client.IgnoreNotFound(err) == nil && err != nil, "webhook config should have been deleted")
}

func TestEnsureWebhookConfiguration_DisabledNoOpWhenNotExists(t *testing.T) {
	// webhook disabled, and no existing config — should not error
	scheme := newWebhookConfigScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	rs := buildResourceSharding("nonexistent-rs", false)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	err := EnsureWebhookConfiguration(context.Background(), fc, rs, gvr, "ns", "svc")
	require.NoError(t, err, "deleting non-existent webhook config should be a no-op")
}

// ---------------------------------------------------------------------------
// DeleteWebhookConfiguration
// ---------------------------------------------------------------------------

func TestDeleteWebhookConfiguration_DeletesExisting(t *testing.T) {
	scheme := newWebhookConfigScheme(t)

	existing := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "resource-sharding-del-rs",
		},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	rs := buildResourceSharding("del-rs", false)

	err := DeleteWebhookConfiguration(context.Background(), fc, rs)
	require.NoError(t, err)

	deleted := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err = fc.Get(context.Background(), types.NamespacedName{Name: "resource-sharding-del-rs"}, deleted)
	assert.Error(t, err, "object should have been deleted")
}

func TestDeleteWebhookConfiguration_IgnoresNotFound(t *testing.T) {
	scheme := newWebhookConfigScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	rs := buildResourceSharding("missing-rs", false)

	err := DeleteWebhookConfiguration(context.Background(), fc, rs)
	require.NoError(t, err, "deleting a non-existent config should not error")
}

// ---------------------------------------------------------------------------
// Managed-by label
// ---------------------------------------------------------------------------

func TestEnsureWebhookConfiguration_SetsManagedByLabel(t *testing.T) {
	scheme := newWebhookConfigScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()

	rs := buildResourceSharding("labeled-rs", true)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	require.NoError(t, EnsureWebhookConfiguration(context.Background(), fc, rs, gvr, "ns", "svc"))

	created := &admissionregistrationv1.MutatingWebhookConfiguration{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "resource-sharding-labeled-rs"}, created))

	assert.Equal(t, "resource-sharding-operator", created.Labels["app.kubernetes.io/managed-by"])
}
