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

package controller

import (
	"context"
	"fmt"

	"go.platform-mesh.io/apis/sharding/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func EnsureWebhookConfiguration(ctx context.Context, c client.Client, rs *v1alpha1.ResourceSharding, gvr schema.GroupVersionResource, namespace, serviceName string) error {
	if !rs.Spec.Webhook.Enabled {
		return DeleteWebhookConfiguration(ctx, c, rs)
	}

	path := "/mutate-shard-assign"
	failurePolicy := admissionregistrationv1.Ignore
	sideEffects := admissionregistrationv1.SideEffectClassNone

	webhookCfg := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("resource-sharding-%s", rs.Name),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "resource-sharding-operator",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: v1alpha1.GroupVersion.String(),
					Kind:       "ResourceSharding",
					Name:       rs.Name,
					UID:        rs.UID,
					Controller: ptr.To(true),
				},
			},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name:                    fmt.Sprintf("%s.shard-assign.sharding.platform-mesh.io", rs.Name),
				FailurePolicy:           &failurePolicy,
				SideEffects:             &sideEffects,
				AdmissionReviewVersions: []string{"v1"},
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Name:      serviceName,
						Namespace: namespace,
						Path:      &path,
					},
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{gvr.Group},
							APIVersions: []string{gvr.Version},
							Resources:   []string{gvr.Resource},
						},
					},
				},
			},
		},
	}

	existing := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err := c.Get(ctx, types.NamespacedName{Name: webhookCfg.Name}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		return c.Create(ctx, webhookCfg)
	}

	existing.Webhooks = webhookCfg.Webhooks
	existing.OwnerReferences = webhookCfg.OwnerReferences
	return c.Update(ctx, existing)
}

func DeleteWebhookConfiguration(ctx context.Context, c client.Client, rs *v1alpha1.ResourceSharding) error {
	webhookCfg := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("resource-sharding-%s", rs.Name),
		},
	}
	err := c.Delete(ctx, webhookCfg)
	return client.IgnoreNotFound(err)
}
