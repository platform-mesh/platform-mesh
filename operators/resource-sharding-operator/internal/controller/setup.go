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
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/discovery"
)

type SetupOptions struct {
	WebhookEnabled bool
}

func SetupWithManager(mgr ctrl.Manager, opts ...SetupOptions) error {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	registry := NewDynamicControllerRegistry()

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = corev1.NamespaceDefault
	}
	serviceName := os.Getenv("WEBHOOK_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "resource-sharding-operator-webhook"
	}

	reconciler := &ResourceShardingReconciler{
		Client:             mgr.GetClient(),
		Discovery:          discoveryClient,
		Registry:           registry,
		Manager:            mgr,
		WebhookNamespace:   namespace,
		WebhookServiceName: serviceName,
	}

	if err := reconciler.SetupWithManager(mgr); err != nil {
		return err
	}

	webhookEnabled := len(opts) > 0 && opts[0].WebhookEnabled
	if webhookEnabled {
		webhookServer := mgr.GetWebhookServer()
		webhookServer.Register("/mutate-shard-assign", &webhook.Admission{
			Handler: &ShardAssignHandler{Registry: registry},
		})
	}

	return nil
}
