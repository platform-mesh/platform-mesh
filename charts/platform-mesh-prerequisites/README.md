# platform-mesh-prerequisites

Optional infrastructure prerequisites for Platform Mesh

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Optimized Kind prerequisites

For the operator's `kind-optimized` profile, add these Helm options to
both prerequisite installation passes, including when using the published OCI
chart. No separate values file is required:

```shell
--set flux2.kustomizeController.create=false \
--set flux2.notificationController.create=false \
--set keycloak-operator.deployment.resources.requests.memory=384Mi \
--set ocm-k8s-toolkit.manager.resources.requests.memory=128Mi
```

These settings omit the unused Flux Kustomize and notification controllers and
reduce the Keycloak operator and OCM manager memory requests while retaining
their memory limits. Keep the usual certificate, gateway, and component settings
for each installation pass.

Flux image scanning and update controllers are disabled by default for all
profiles because Platform Mesh selects images through OCM component versions.
Enable `flux2.imageReflectionController.create` and
`flux2.imageAutomationController.create` when using Flux image automation.
Keep Kustomize or notification controllers enabled when an installation uses
Flux Kustomizations, Alerts, Providers, or Receivers.
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| cert-manager.crds.enabled | bool | `true` |  |
| cert-manager.enabled | bool | `true` |  |
| cert-manager.webhook.timeoutSeconds | int | `15` |  |
| cnpg.enabled | bool | `true` |  |
| etcd-druid.enabled | bool | `true` |  |
| flux2.enabled | bool | `true` |  |
| flux2.helmController.container.additionalArgs[0] | string | `"--concurrent=10"` |  |
| flux2.imageAutomationController.create | bool | `false` | Enable Flux image updates when using ImageUpdateAutomation resources. Platform Mesh uses OCM component versions instead. |
| flux2.imageReflectionController.create | bool | `false` | Enable Flux image scanning when using ImageRepository or ImagePolicy resources. |
| flux2.policies.create | bool | `false` |  |
| keycloak-operator.enabled | bool | `true` |  |
| localCertificates.caSecretName | string | `"domain-certificate-ca"` |  |
| localCertificates.certificateSecretName | string | `"domain-certificate"` |  |
| localCertificates.dnsNames | list | `[]` |  |
| localCertificates.enabled | bool | `false` |  |
| localCertificates.namespace | string | `"platform-mesh-system"` |  |
| ocm-k8s-toolkit.enabled | bool | `true` |  |
| traefik-crds.enabled | bool | `true` |  |
| traefik-crds.gatewayAPI | bool | `false` |  |
| traefik-crds.gatewayAPIExperimental | bool | `true` |  |
| traefik-crds.traefik | bool | `true` |  |
| traefik.enabled | bool | `false` |  |
| traefik.gateway.enabled | bool | `false` |  |
| traefik.gatewayClass.enabled | bool | `true` |  |
| traefik.providers.kubernetesGateway.enabled | bool | `true` |  |

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
