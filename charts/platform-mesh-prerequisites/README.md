# platform-mesh-prerequisites

Optional infrastructure prerequisites for Platform Mesh

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
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
| flux2.policies.create | bool | `false` |  |
| gateway-api-crds.enabled | bool | `true` |  |
| keycloak-operator.enabled | bool | `true` |  |
| localCertificates.caSecretName | string | `"domain-certificate-ca"` |  |
| localCertificates.certificateSecretName | string | `"domain-certificate"` |  |
| localCertificates.dnsNames | list | `[]` |  |
| localCertificates.enabled | bool | `false` |  |
| localCertificates.namespace | string | `"platform-mesh-system"` |  |
| ocm-k8s-toolkit.enabled | bool | `true` |  |
| traefik-crds.enabled | bool | `true` |  |
| traefik-crds.gatewayAPI | bool | `false` |  |
| traefik.enabled | bool | `false` |  |
| traefik.gatewayClass.enabled | bool | `true` |  |
| traefik.providers.kubernetesGateway.enabled | bool | `true` |  |
| traefik.providers.kubernetesGateway.experimentalChannel | bool | `true` |  |

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
# platform-mesh-prerequisites

![Version: 0.0.0](https://img.shields.io/badge/Version-0.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

Optional infrastructure prerequisites for Platform Mesh

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| file://../gateway-api-crds | gateway-api-crds | 0.0.0 |
| file://../keycloak-operator | keycloak-operator | 0.0.0 |
| oci://ghcr.io/cloudnative-pg/charts | cloudnative-pg | 0.28.0 |
| oci://ghcr.io/fluxcd-community/charts | flux2 | 2.17.2 |
| oci://ghcr.io/open-component-model/kubernetes/controller | ocm-k8s-toolkit(chart) | 0.13.0 |
| oci://ghcr.io/platform-mesh/charts/gardener | etcd-druid | v0.39.1 |
| oci://ghcr.io/platform-mesh/ocm/charts | traefik-crds | 1.14.0 |
| oci://ghcr.io/traefik/helm | traefik | 42.1.3 |
| oci://quay.io/jetstack/charts | cert-manager | v1.20.5 |

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
| flux2.policies.create | bool | `false` |  |
| gateway-api-crds.enabled | bool | `true` |  |
| keycloak-operator.enabled | bool | `true` |  |
| localCertificates.caSecretName | string | `"domain-certificate-ca"` |  |
| localCertificates.certificateSecretName | string | `"domain-certificate"` |  |
| localCertificates.dnsNames | list | `[]` |  |
| localCertificates.enabled | bool | `false` |  |
| localCertificates.namespace | string | `"platform-mesh-system"` |  |
| ocm-k8s-toolkit.enabled | bool | `true` |  |
| traefik-crds.enabled | bool | `true` |  |
| traefik-crds.gatewayAPI | bool | `false` |  |
| traefik.enabled | bool | `false` |  |
| traefik.gatewayClass.enabled | bool | `true` |  |
| traefik.providers.kubernetesGateway.enabled | bool | `true` |  |
| traefik.providers.kubernetesGateway.experimentalChannel | bool | `true` |  |

