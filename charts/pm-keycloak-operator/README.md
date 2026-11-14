# pm-keycloak-operator

Manages the Keycloak identity provider of a Platform Mesh installation (keycloak.platform-mesh.io)

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| baseDomain | string | `""` | Base domain of the platform deployment (e.g. platform.example.com). Used to derive internal service URLs when explicit URLs are not set. Required. |
| caSecret | string | `""` | Name of the secret containing the CA certificate for outbound TLS trust (key: tls.crt or ca.crt) |
| clusterScopedName.prefix | string | `""` | Prefix for the names of cluster-scoped resources (ClusterRoles, ClusterRoleBindings, webhook configurations), so several installations can share a cluster. A global.clusterScopedName value takes precedence over this one |
| clusterScopedName.suffix | string | `""` | Suffix for the names of cluster-scoped resources |
| deployment.resources.limits.cpu | string | `"260m"` |  |
| deployment.resources.limits.memory | string | `"512Mi"` |  |
| deployment.resources.requests.cpu | string | `"150m"` |  |
| deployment.resources.requests.memory | string | `"128Mi"` |  |
| deployment.revisionHistoryLimit | int | `3` | Number of old ReplicaSets to retain for rollback |
| domainCALookup | bool | `true` | Enable domain CA lookup for TLS verification on kcp side |
| environment | string | `""` | environment indicator, used for logging and observability |
| featureGates | object | `{}` |  |
| hostAliases.enabled | bool | `false` |  |
| idp.additionalRedirectUrls | list | `[]` | Additional redirect URLs to allow for OAuth clients (e.g. for local development callbacks) |
| idp.allowUnverifiedEmails | Development only | `false` | Allow login without email verification. Must be false in production. |
| idp.registrationAllowed | Development only | `false` | Allow self-registration of new users via IDP. Must be false in production. |
| idp.setDefaultPassword | Development only | `false` | Set a default password for new users. Must be false in production. |
| idp.smtp.fromAddress | string | `""` | Email address used as the sender for IDP notifications |
| idp.smtp.port | string | `""` | SMTP server port |
| idp.smtp.server | string | `""` | SMTP server hostname |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/pm-keycloak-operator"` | The image repository path (without registry) |
| initContainer | object | `{"clients":[{"name":"pm-keycloak-operator","secretRef":{"name":"pm-keycloak-operator-client-secret","namespace":"platform-mesh-system"}},{"name":"iam-service","secretRef":{"name":"iam-client-secret","namespace":"platform-mesh-system"}}],"enabled":true,"keycloakClientId":"admin-cli","keycloakUser":"keycloak-admin"}` | Init container configuration for bootstrapping Keycloak service account clients |
| initContainer.clients | list | `[{"name":"pm-keycloak-operator","secretRef":{"name":"pm-keycloak-operator-client-secret","namespace":"platform-mesh-system"}},{"name":"iam-service","secretRef":{"name":"iam-client-secret","namespace":"platform-mesh-system"}}]` | Service account clients to create (beyond the default one from keycloak.client.secret) |
| initContainer.enabled | bool | `true` | Enable the init container |
| initContainer.keycloakClientId | string | `"admin-cli"` | Keycloak client ID for admin authentication |
| initContainer.keycloakUser | string | `"keycloak-admin"` | Keycloak username for admin authentication |
| initializer.extraArgs | list | `[]` |  |
| initializer.kubeconfigSecret | string | `"keycloak-initializer-kubeconfig"` | The kubeconfig secret for the initializer |
| initializer.subroutines.idpEnabled | bool | `true` | Enable IDPSubroutine (Keycloak identity provider configuration) |
| initializer.subroutines.inviteEnabled | bool | `true` | Enable InviteSubroutine (creates Invite resources for org creator) |
| initializer.subroutines.workspaceAuthEnabled | bool | `true` | Enable WorkspaceAuthConfigurationSubroutine (JWT authentication setup) |
| jwt.userIdClaim | string | `"email"` |  |
| keycloak.baseUrl | string | `""` | base URL of the Keycloak instance (e.g. https://example.com/keycloak). Required. |
| keycloak.client.secret.key | string | `"client_secret"` |  |
| keycloak.client.secret.name | string | `"pm-keycloak-operator-client-secret"` |  |
| keycloakSecret | string | `"keycloak-admin"` | Name of the secret containing Keycloak admin credentials (keys: username, password, secret) |
| kubeconfigSecret | string | `""` | The kubeconfig secret for the operator |
| operator.extraArgs | list | `[]` |  |
| region | string | `""` | region indicator, used for logging and observability |
| terminator.extraArgs | list | `[]` |  |
| terminator.kubeconfigSecret | string | `"keycloak-terminator-kubeconfig"` | The kubeconfig secret for the terminator |
| webhooks.caDuration | string | `"2160h"` | CA certificate duration (default: 3 months) |
| webhooks.caRenewBefore | string | `"720h"` | CA certificate renewal time before expiration (default: 30 days) |
| webhooks.certDir | string | `"/certs"` | The directory for webhook certificates (mounted from the serving cert secret) |
| webhooks.certDuration | string | `"2160h"` | Serving certificate duration (default: 3 months) |
| webhooks.certRenewBefore | string | `"720h"` | Serving certificate renewal time before expiration (default: 30 days) |
| webhooks.enabled | bool | `true` | Enable validating admission webhooks |
| webhooks.port | int | `9443` | Webhook server port (must match controller-runtime webhook server) |
| webhooks.register | bool | `false` | Register the ValidatingWebhookConfiguration resources |

## Overriding Values

The values in the `defaults:` section can be reused from other charts by using the lookup function "common.getKeyValue". It implements lookup on three levels:

1. Looks for `keyOverride` in the chart's values.yaml
2. Looks for `global.key` in the chart's or parent chart's values.yaml
3. Uses the `key` in the chart's values.yaml
4. Uses the `common.defaults.key` value from the table below.

1 has precedence over 2 over 3 over 4 respectively. This approach allows for individual charts to have minimal configuration, while still being able to override parameters locally.

Example
```
1) .Values.deployment.resources.limits.memoryOverride = 4096MB
2) .Values.global.deployment.resources.limits.memory = 2048MB
3) .Values.deployment.resources.limits.memory = 1024MB
4) .Values.common.defaults.deployment.resources.limits.memory = default 512MB
```
