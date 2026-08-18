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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	accountWebhookConfigurationName = "organization-validator.webhooks.core.platform-mesh.io"
	accountWebhookServingSecretName = "account-operator-webhook-server-cert"

	identityProviderWebhookConfigurationName = "identityproviderconfiguration-validator.webhooks.core.platform-mesh.io"
	identityProviderWebhookServingSecretName = "security-operator-webhook-server-cert"

	identityBootstrapRestartAnnotation      = "restore.platform-mesh.io/identity-bootstrap-restarted-v1-for"
	organizationControllerRestartAnnotation = "restore.platform-mesh.io/organization-controller-restarted-v1-for"
	organizationReconcileAnnotation         = "restore.platform-mesh.io/force-reconcile-v1-for"
	workspaceTypeCollisionRepairAnnotation  = "restore.platform-mesh.io/workspace-type-collision-repaired-v1-for"
)

var (
	accountGVR = schema.GroupVersionResource{
		Group:    "core.platform-mesh.io",
		Version:  "v1alpha1",
		Resource: "accounts",
	}
	validatingWebhookConfigurationGVR = schema.GroupVersionResource{
		Group:    "admissionregistration.k8s.io",
		Version:  "v1",
		Resource: "validatingwebhookconfigurations",
	}
	identityProviderConfigurationGVR = schema.GroupVersionResource{
		Group:    "core.platform-mesh.io",
		Version:  "v1alpha1",
		Resource: "identityproviderconfigurations",
	}
	workspaceTypeGVR = schema.GroupVersionResource{
		Group:    "tenancy.kcp.io",
		Version:  "v1alpha1",
		Resource: "workspacetypes",
	}
)

type organizationWebhookTrust struct {
	configurationName string
	servingSecretName string
}

var organizationWebhookTrusts = []organizationWebhookTrust{
	{
		configurationName: accountWebhookConfigurationName,
		servingSecretName: accountWebhookServingSecretName,
	},
	{
		configurationName: identityProviderWebhookConfigurationName,
		servingSecretName: identityProviderWebhookServingSecretName,
	},
}

// recoverOrganizations sequences the existing organization recovery helpers
// without changing their shared use by identity validation.
func (p *PlatformRecoverySubroutine) recoverOrganizations(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (recoveryWait, error) {
	log := logger.LoadLoggerFromContext(ctx)
	log.Info().Str("step", "organization-webhook-trust").Msg("starting platform recovery step")
	changed, err := p.ensureOrganizationAdmissionWebhookTrust(ctx)
	if err != nil {
		return recoveryWait{}, err
	}
	if changed {
		return recoveryWait{5 * time.Second, "waiting after organization admission webhook CA repair"}, nil
	}
	controllersReady, err := p.ensureOrganizationControllersReady(ctx, pr)
	if err != nil {
		return recoveryWait{}, fmt.Errorf("recover organization controllers: %w", err)
	}
	if !controllersReady {
		return recoveryWait{10 * time.Second, "waiting for organization controllers to restart"}, nil
	}
	log.Info().Str("step", "organizations").Msg("starting platform recovery step")
	organizationsReady, err := p.restoredOrganizationResourcesReady(ctx, pr)
	if err != nil {
		return recoveryWait{}, err
	}
	if !organizationsReady {
		return recoveryWait{10 * time.Second, "waiting for restored accounts and identity providers to become ready"}, nil
	}
	return recoveryWait{}, nil
}

// ensureOrganizationAdmissionWebhookTrust replaces CA bundles restored with
// KCP etcd with the CAs used by the destination cluster's webhook pods.
func (p *PlatformRecoverySubroutine) ensureOrganizationAdmissionWebhookTrust(ctx context.Context) (bool, error) {
	dynamicClient, err := p.kcpRecoveryDynamicClient(ctx, platformSystemPath)
	if err != nil {
		return false, fmt.Errorf("create KCP provider-workspace recovery client: %w", err)
	}

	changed := false
	for _, trust := range organizationWebhookTrusts {
		var secret corev1.Secret
		if err := p.client.Get(ctx, ctrlruntimeclient.ObjectKey{
			Namespace: platformMeshNamespace,
			Name:      trust.servingSecretName,
		}, &secret); err != nil {
			return false, fmt.Errorf("get webhook serving Secret %s/%s: %w", platformMeshNamespace, trust.servingSecretName, err)
		}

		caBundle := secret.Data["ca.crt"]
		if len(caBundle) == 0 {
			return false, fmt.Errorf("webhook serving Secret %s/%s has no ca.crt", platformMeshNamespace, trust.servingSecretName)
		}

		updated, err := ensureValidatingWebhookCABundle(ctx, dynamicClient, trust.configurationName, caBundle)
		if err != nil {
			return false, err
		}
		changed = changed || updated
	}

	return changed, nil
}

func ensureValidatingWebhookCABundle(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	configurationName string,
	caBundle []byte,
) (bool, error) {
	resource := dynamicClient.Resource(validatingWebhookConfigurationGVR)
	configuration, err := resource.Get(ctx, configurationName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get KCP ValidatingWebhookConfiguration %s: %w", configurationName, err)
	}

	webhooks, found, err := unstructured.NestedSlice(configuration.Object, "webhooks")
	if err != nil {
		return false, fmt.Errorf("read webhooks from KCP ValidatingWebhookConfiguration %s: %w", configurationName, err)
	}
	if !found || len(webhooks) == 0 {
		return false, fmt.Errorf("KCP ValidatingWebhookConfiguration %s has no webhooks", configurationName)
	}

	desired := base64.StdEncoding.EncodeToString(caBundle)
	changed := false
	for index := range webhooks {
		webhook, ok := webhooks[index].(map[string]any)
		if !ok {
			return false, fmt.Errorf("KCP ValidatingWebhookConfiguration %s has an invalid webhook at index %d", configurationName, index)
		}
		clientConfig, ok := webhook["clientConfig"].(map[string]any)
		if !ok {
			return false, fmt.Errorf("KCP ValidatingWebhookConfiguration %s webhook %d has no clientConfig", configurationName, index)
		}
		if current, _ := clientConfig["caBundle"].(string); current == desired {
			continue
		}
		clientConfig["caBundle"] = desired
		changed = true
	}

	if !changed {
		return false, nil
	}
	if err := unstructured.SetNestedSlice(configuration.Object, webhooks, "webhooks"); err != nil {
		return false, fmt.Errorf("write webhooks to KCP ValidatingWebhookConfiguration %s: %w", configurationName, err)
	}
	if _, err := resource.Update(ctx, configuration, metav1.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("update KCP ValidatingWebhookConfiguration %s: %w", configurationName, err)
	}

	return true, nil
}

// ensureOrganizationControllersReady deliberately restarts the security
// bootstrap pod before the IDP controller. The bootstrap init container writes
// the current Keycloak client secret, and security-operator-system must start
// afterwards so its environment contains that value.
func (p *PlatformRecoverySubroutine) ensureOrganizationControllersReady(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	bootstrap := platformDeployment("security-operator")
	restarted, err := restartDeployment(
		ctx,
		p.client,
		bootstrap,
		identityBootstrapRestartAnnotation,
		string(pr.UID),
	)
	if err != nil {
		return false, fmt.Errorf("restart identity bootstrap controller: %w", err)
	}
	if restarted {
		return false, nil
	}

	ready, err := bootstrap.ready(ctx, p.client)
	if err != nil {
		return false, fmt.Errorf("check identity bootstrap controller: %w", err)
	}
	if !ready {
		return false, nil
	}

	controllers := []workloadRef{
		platformDeployment("account-operator"),
		platformDeployment("security-operator-system"),
	}
	controllersRestarted := false
	for _, controller := range controllers {
		restarted, err := restartDeployment(
			ctx,
			p.client,
			controller,
			organizationControllerRestartAnnotation,
			string(pr.UID),
		)
		if err != nil {
			return false, fmt.Errorf("restart organization controller %s/%s: %w", controller.Namespace, controller.Name, err)
		}
		controllersRestarted = controllersRestarted || restarted
	}
	if controllersRestarted {
		return false, nil
	}

	for _, controller := range controllers {
		ready, err := controller.ready(ctx, p.client)
		if err != nil {
			return false, fmt.Errorf("check organization controller %s/%s: %w", controller.Namespace, controller.Name, err)
		}
		if !ready {
			return false, nil
		}
	}

	return true, nil
}

func (p *PlatformRecoverySubroutine) restoredOrganizationResourcesReady(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
) (bool, error) {
	dynamicClient, err := p.kcpRecoveryDynamicClient(ctx, orgsLogicalClusterPath)
	if err != nil {
		return false, err
	}

	ready, _, err := ensureOrganizationResourcesReady(ctx, pr, dynamicClient)
	return ready, err
}

func ensureOrganizationResourcesReady(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
	dynamicClient dynamic.Interface,
) (bool, bool, error) {
	log := logger.LoadLoggerFromContext(ctx)
	allReady := true
	changed := false

	resources := []struct {
		kind string
		gvr  schema.GroupVersionResource
	}{
		{kind: "Account", gvr: accountGVR},
		{kind: "IdentityProviderConfiguration", gvr: identityProviderConfigurationGVR},
	}

	for _, target := range resources {
		resource := dynamicClient.Resource(target.gvr)
		objects, err := resource.List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, changed, fmt.Errorf("list restored %s resources: %w", target.kind, err)
		}

		for index := range objects.Items {
			object := &objects.Items[index]
			if object.GetDeletionTimestamp() != nil {
				continue
			}

			ready := unstructuredConditionTrue(object, "Ready")
			if target.kind == "Account" && !ready {
				repaired, err := repairRestoredAccountWorkspaceTypeCollision(ctx, pr, dynamicClient, object)
				if err != nil {
					return false, changed, err
				}
				if repaired {
					log.Info().
						Str("subroutine", platformRecoverySubroutineName).
						Str("platformRestore", pr.Name).
						Str("account", object.GetName()).
						Msg("removed restored Account WorkspaceTypes after already-exists reconciliation collision")
					changed = true
					allReady = false
					continue
				}
			}

			annotations := object.GetAnnotations()
			marker := ""
			if annotations != nil {
				marker = annotations[organizationReconcileAnnotation]
			}
			restoreMarker := string(pr.UID)
			touchedForRestore := marker == restoreMarker || strings.HasPrefix(marker, restoreMarker+"-")

			// Touch every restored resource once after the destination controllers
			// are ready. Repeatedly touching a failed Account can race its
			// path-aware KCP cache and turn an already-existing WorkspaceType into
			// a permanent create conflict.
			if !touchedForRestore {
				patch, err := json.Marshal(map[string]any{
					"metadata": map[string]any{
						"annotations": map[string]string{
							organizationReconcileAnnotation: restoreMarker,
						},
					},
				})
				if err != nil {
					return false, changed, fmt.Errorf("build %s %s reconcile patch: %w", target.kind, object.GetName(), err)
				}
				if _, err := resource.Patch(ctx, object.GetName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
					return false, changed, fmt.Errorf("trigger restored %s %s reconciliation: %w", target.kind, object.GetName(), err)
				}

				log.Info().
					Str("subroutine", platformRecoverySubroutineName).
					Str("platformRestore", pr.Name).
					Str("kind", target.kind).
					Str("resource", object.GetName()).
					Bool("ready", ready).
					Msg("triggered restored organization resource reconciliation")
				changed = true
				allReady = false
				continue
			}
		}
	}

	return allReady, changed, nil
}

func repairRestoredAccountWorkspaceTypeCollision(
	ctx context.Context,
	pr *v1alpha1.PlatformRestore,
	dynamicClient dynamic.Interface,
	account *unstructured.Unstructured,
) (bool, error) {
	if !accountWorkspaceTypeAlreadyExists(account) {
		return false, nil
	}

	if account.GetAnnotations()[workspaceTypeCollisionRepairAnnotation] == string(pr.UID) {
		return false, nil
	}

	for _, name := range []string{account.GetName() + "-org", account.GetName() + "-account"} {
		if err := dynamicClient.Resource(workspaceTypeGVR).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("delete restored WorkspaceType %s for Account %s: %w", name, account.GetName(), err)
		}
	}

	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]string{
				workspaceTypeCollisionRepairAnnotation: string(pr.UID),
			},
		},
	})
	if err != nil {
		return false, fmt.Errorf("build Account %s WorkspaceType collision repair patch: %w", account.GetName(), err)
	}
	if _, err := dynamicClient.Resource(accountGVR).Patch(ctx, account.GetName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return false, fmt.Errorf("mark Account %s WorkspaceType collision repair: %w", account.GetName(), err)
	}

	return true, nil
}

func accountWorkspaceTypeAlreadyExists(account *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(account.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok || condition["type"] != "WorkspaceTypeSubroutine" || condition["status"] != "False" {
			continue
		}
		message, _ := condition["message"].(string)
		if strings.Contains(message, "workspacetypes.tenancy.kcp.io") && strings.Contains(message, "already exists") {
			return true
		}
	}

	return false
}
