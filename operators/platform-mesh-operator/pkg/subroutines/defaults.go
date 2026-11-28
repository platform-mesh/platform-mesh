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

// DefaultImageResources defines each service's OCM image mappings.
var DefaultImageResources = []ServiceDefaults{
	// Platform Mesh operators.
	{
		ServiceName: pmcorev1alpha1.AccountOperator,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "account-operator",
					Unsuspend: "true",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.ExtensionManagerOperator,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "extension-manager-operator",
					Unsuspend: "true",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.PMKeycloakOperator,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "pm-keycloak-operator",
					Unsuspend: "true",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.PMOpenFGAOperator,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "pm-openfga-operator",
					Unsuspend: "true",
				},
			},
		},
	},

	// Platform Mesh services.
	{
		ServiceName: pmcorev1alpha1.Infra,
		ImageResources: []ImageResource{
			{
				Name:         "keycloak-image",
				Resource:     "keycloak-image",
				ResourceName: "keycloak-image",
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "infra",
					Path:     "keycloak.operator.image.tag",
				},
			},
			{
				Name:         "busybox-image",
				Resource:     "busybox-image",
				ResourceName: "busybox-image",
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "infra",
					Path:     "keycloak.operator.waitForDb.image.tag",
				},
			},
			{
				Name:         "postgres-image",
				Resource:     "postgres-image",
				ResourceName: "postgres-image",
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "infra",
					Path:     "cnpg.cluster.image.tag",
				},
			},
			{
				Name:     "kcp-server-image",
				Resource: "image",
				AbsoluteReferencePath: []pmcorev1alpha1.ReferencePathElement{
					{Name: "kcp"},
				},
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "infra",
					Path:     "kcp.image.tag",
					ImageRef: "combined",
				},
			},
			{
				Name:     "etcd-wrapper-image",
				Resource: "etcd-wrapper-image",
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "infra",
					Path:     "kcp.etcd.image.tag",
					ImageRef: "combined",
				},
			},
			{
				Name:     "etcdbrctl-image",
				Resource: "etcdbrctl-image",
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "infra",
					Path:     "kcp.etcd.backup.image.tag",
					ImageRef: "combined",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.KubernetesGraphQLGateway,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "kubernetes-graphql-gateway",
					Unsuspend: "true",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.IAMService,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "iam-service",
					Unsuspend: "true",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.PMOpenFGAAuthzWebhook,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "pm-openfga-authz-webhook",
					Unsuspend: "true",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.ExtensionManagerVirtualWorkspace,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "extension-manager-virtual-workspace",
					Unsuspend: "true",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.Marketplace,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "marketplace",
					Unsuspend: "true",
				},
			},
		},
	},

	// UI.
	{
		ServiceName: pmcorev1alpha1.Portal,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "portal",
					Unsuspend: "true",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.MarketplaceUI,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "marketplace-ui",
					Unsuspend: "true",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.IAMUI,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "iam-ui",
					Unsuspend: "true",
				},
			},
		},
	},

	// Third party tools.
	{
		ServiceName: pmcorev1alpha1.OpenFGA,
		ImageResources: []ImageResource{
			{
				Name: "openfga-image",
				Annotations: ImageResourceAnnotations{
					Repo:      "oci",
					Artifact:  "image",
					For:       "openfga",
					ImageRef:  "combined",
					Unsuspend: "true",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.KCPOperator,
		ImageResources: []ImageResource{
			{
				Name: "kcp-operator-image",
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "kcp-operator",
					ImageRef: "combined",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.KeycloakOperator,
		ImageResources: []ImageResource{
			{
				Name:         "keycloak-operator-keycloak-image",
				Resource:     "keycloak-image",
				ResourceName: "keycloak-image",
				AbsoluteReferencePath: []pmcorev1alpha1.ReferencePathElement{
					{Name: "keycloak-operator"},
					{Name: "image"},
				},
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "keycloak-operator",
					Path:     "keycloakImage.tag",
				},
			},
			{
				Name:         "keycloak-operator-image",
				Resource:     "manager-image",
				ResourceName: "manager-image",
				AbsoluteReferencePath: []pmcorev1alpha1.ReferencePathElement{
					{Name: "keycloak-operator"},
					{Name: "image"},
				},
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "keycloak-operator",
					Path:     "image.tag",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.CNPGOperator,
		ImageResources: []ImageResource{
			{
				Name: "cnpg-operator-image",
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "cnpg-operator",
					ImageRef: "combined",
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.TerminalControllerManager,
		ImageResources: []ImageResource{
			{
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "terminal-controller-manager",
				},
			},
			{
				Name: "terminal-image",
				Annotations: ImageResourceAnnotations{
					Path:     "terminal.image.tag",
					Repo:     "oci",
					Artifact: "image",
					For:      "terminal-controller-manager",
				},
				Resource: "terminal-image",
				ReferencePath: []pmcorev1alpha1.ReferencePathElement{
					{Name: "terminal-controller-manager"},
				},
			},
		},
	},
	{
		ServiceName: pmcorev1alpha1.Observability,
		ImageResources: []ImageResource{
			{
				Name:         "otel-collector-image",
				Resource:     "otel-collector-image",
				ResourceName: "otel-collector-image",
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "observability",
					Path:     "otelCollector.image.tag",
				},
			},
			{
				Name:     "prometheus-server-image",
				Resource: "prometheus-server-image",
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "observability",
					Path:     "prometheus.server.image.tag",
					ImageRef: "combined",
				},
			},
			{
				Name:     "prometheus-config-reloader-image",
				Resource: "prometheus-config-reloader-image",
				Annotations: ImageResourceAnnotations{
					Repo:     "oci",
					Artifact: "image",
					For:      "observability",
					Path:     "prometheus.configmapReload.prometheus.image.tag",
					ImageRef: "combined",
				},
			},
		},
	},
}
