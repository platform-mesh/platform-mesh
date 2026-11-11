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

package subroutines

import (
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const DefaultRequeueInterval = 5 * time.Second

var AccountOperatorWebhookSecretName = "account-operator-webhook-server-cert"
var AccountOperatorWebhookSecretNamespace = "platform-mesh-system"

var DefaultCASecretKey = "ca.crt"
var AccountOperatorMutatingWebhookName = "account-operator.webhooks.core.platform-mesh.io"
var AccountOperatorValidatingWebhookName = "organization-validator.webhooks.core.platform-mesh.io"

// KeycloakOperatorWebhookCASecretName is the CA secret of the pm-keycloak-operator webhook chart PKI.
var KeycloakOperatorWebhookCASecretName = "pm-keycloak-operator-ca-secret"
var IdentityProviderValidatingWebhookName = "identityproviderconfiguration-validator.webhooks.core.platform-mesh.io"
var AccountOperatorWorkspace = "root:platform-mesh-system"

const FeatureDisableIDPWebhook = "feature-disable-idp-webhook"

const FeatureDisableKeycloak = "feature-disable-keycloak"

const FeatureDisableOpenFGA = "feature-disable-openfga"

// FeatureToggleConflicts lists field paths below the services that must
// be off when a feature toggle is set.
// When a PlatformMesh disables a feature and the profile has one of
// them on, the reconciliation is stopped and a condition is set on the
// resource.
// If the postgres for either openfga or keycloak are managed by
// pm-operator they are not deleted even if the respective feature is
// disabled. They must be deleted manually by the administrator.
var FeatureToggleConflicts = map[string][][]string{
	FeatureDisableKeycloak: {
		{"pm-keycloak-operator", "enabled"},
		{"iam-service", "enabled"},
		{"iam-ui", "enabled"},
		{"infra", "values", "keycloak", "operator", "enabled"},
	},
	FeatureDisableOpenFGA: {
		{"pm-openfga-operator", "enabled"},
		{"pm-openfga-authz-webhook", "enabled"},
		{"iam-service", "enabled"},
		{"iam-ui", "enabled"},
		{"openfga", "enabled"},
		{"infra", "values", "kcp", "webhook", "enabled"},
	},
}

const OpenfgaAuthzWebhookKubeconfigSecret = "pm-openfga-authz-webhook-kubeconfig"

var DefaultProviderConnections = []pmcorev1alpha1.ProviderConnection{
	{
		Path:      "root:platform-mesh-system",
		Secret:    "account-operator-kubeconfig",
		AdminAuth: ptr.To(true),
	},
	{
		Path:           "root:platform-mesh-system",
		Secret:         OpenfgaAuthzWebhookKubeconfigSecret,
		APIExportNames: []string{"core.platform-mesh.io", "fga.platform-mesh.io", "openfga.platform-mesh.io"},
		AdminAuth:      ptr.To(false),
	},
	{
		Path:      "root:platform-mesh-system",
		Secret:    "pm-openfga-operator-kubeconfig",
		AdminAuth: ptr.To(true),
	},
	{
		Path:      "root:platform-mesh-system",
		Secret:    "pm-keycloak-operator-kubeconfig",
		AdminAuth: ptr.To(true),
	},
	{
		Path:      "root:platform-mesh-system",
		Secret:    "kubernetes-graphql-gateway-kubeconfig",
		AdminAuth: ptr.To(true),
	},
	{
		RawPath:   ptr.To("/services/marketplace"),
		Secret:    "virtual-workspace-clusteraccess-kubeconfig",
		AdminAuth: ptr.To(true),
	},
	{
		Path:           "root:platform-mesh-system",
		Secret:         "extension-manager-operator-kubeconfig",
		APIExportNames: []string{"core.platform-mesh.io", "ui.platform-mesh.io"},
		AdminAuth:      ptr.To(false),
	},
	{
		Path:           "root:platform-mesh-system",
		Secret:         "iam-service-kubeconfig",
		APIExportNames: []string{"core.platform-mesh.io", "providers.platform-mesh.io", "keycloak.platform-mesh.io"},
		AdminAuth:      ptr.To(false),
	},
	{
		Path:      "root:orgs",
		RawPath:   ptr.To("/services/contentconfigurations"),
		Secret:    "portal-kubeconfig",
		AdminAuth: ptr.To(true),
	},
	{
		Path:      "root",
		Secret:    "openfga-initializer-kubeconfig",
		AdminAuth: ptr.To(true),
	},
	{
		Path:      "root",
		Secret:    "openfga-terminator-kubeconfig",
		AdminAuth: ptr.To(true),
	},
	{
		Path:      "root",
		Secret:    "keycloak-initializer-kubeconfig",
		AdminAuth: ptr.To(true),
	},
	{
		Path:      "root",
		Secret:    "keycloak-terminator-kubeconfig",
		AdminAuth: ptr.To(true),
	},
	{
		Path:      "root:platform-mesh-system",
		Secret:    "virtual-workspaces-kubeconfig",
		AdminAuth: ptr.To(true),
	},
}

var DEFAULT_WEBHOOK_CONFIGURATION = pmcorev1alpha1.WebhookConfiguration{
	SecretRef: pmcorev1alpha1.SecretReference{
		Name:      AccountOperatorWebhookSecretName,
		Namespace: AccountOperatorWebhookSecretNamespace,
	},
	SecretData: DefaultCASecretKey,
	WebhookRef: pmcorev1alpha1.KCPAPIVersionKindRef{
		ApiVersion: "admissionregistration.k8s.io/v1",
		Kind:       "MutatingWebhookConfiguration",
		Name:       AccountOperatorMutatingWebhookName,
		Path:       AccountOperatorWorkspace,
	},
}

var DEFAULT_VALIDATING_WEBHOOK_CONFIGURATION = pmcorev1alpha1.WebhookConfiguration{
	SecretRef: pmcorev1alpha1.SecretReference{
		Name:      AccountOperatorWebhookSecretName,
		Namespace: AccountOperatorWebhookSecretNamespace,
	},
	SecretData: DefaultCASecretKey,
	WebhookRef: pmcorev1alpha1.KCPAPIVersionKindRef{
		ApiVersion: "admissionregistration.k8s.io/v1",
		Kind:       "ValidatingWebhookConfiguration",
		Name:       AccountOperatorValidatingWebhookName,
		Path:       AccountOperatorWorkspace,
	},
}

var DEFAULT_IDENTITY_PROVIDER_VALIDATING_WEBHOOK_CONFIGURATION = pmcorev1alpha1.WebhookConfiguration{
	SecretRef: pmcorev1alpha1.SecretReference{
		Name:      KeycloakOperatorWebhookCASecretName,
		Namespace: AccountOperatorWebhookSecretNamespace,
	},
	SecretData: DefaultCASecretKey,
	WebhookRef: pmcorev1alpha1.KCPAPIVersionKindRef{
		ApiVersion: "admissionregistration.k8s.io/v1",
		Kind:       "ValidatingWebhookConfiguration",
		Name:       IdentityProviderValidatingWebhookName,
		Path:       AccountOperatorWorkspace,
	},
}

var DEFAULT_WAIT_CONFIG = pmcorev1alpha1.WaitConfig{
	ResourceTypes: []pmcorev1alpha1.ResourceType{
		{
			GroupVersionKind: metav1.GroupVersionKind{
				Group:   "helm.toolkit.fluxcd.io",
				Version: "v2",
				Kind:    "HelmRelease",
			},
			Namespace: "default",
			LabelSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "core.platform-mesh.io/operator-created",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"true"},
					},
				},
			},
			ConditionStatus:  metav1.ConditionTrue,
			RowConditionType: "Ready",
		},
	},
}
