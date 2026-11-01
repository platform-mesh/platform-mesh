# example-httpbin-operator

A Helm chart for deploying the httpbin operator and its CRDs

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)

## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` |  |
| apiSyncagent.publishedResources.enable | bool | `false` |  |
| apiSyncagent.rbac.enable | bool | `false` |  |
| apiSyncagent.serviceAccount.name | string | `"api-syncagent"` |  |
| apiSyncagent.serviceAccount.namespace | string | `"platform-mesh-system"` |  |
| certmanager.enable | bool | `false` |  |
| controllerManager.serviceAccount.annotations | object | `{}` |  |
| controllerManager.serviceAccountName | string | `"example-httpbin-operator-controller-manager"` |  |
| enabled | bool | `true` |  |
| fullnameOverride | string | `""` |  |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.registry | string | `"ghcr.io"` |  |
| image.repository | string | `"platform-mesh/example-httpbin-operator"` |  |
| metrics.enable | bool | `true` |  |
| nameOverride | string | `""` |  |
| networkPolicy.enable | bool | `false` |  |
| nodeSelector | object | `{}` |  |
| operator.args | list | `[]` |  |
| operator.install | bool | `true` |  |
| operator.remoteKubeconfig | string | `""` |  |
| operator.remoteKubeconfigSubPath | string | `""` |  |
| operator.resources.limits.cpu | string | `"500m"` |  |
| operator.resources.limits.memory | string | `"128Mi"` |  |
| operator.resources.requests.cpu | string | `"100m"` |  |
| operator.resources.requests.memory | string | `"64Mi"` |  |
| podAnnotations | object | `{}` |  |
| podLabels | object | `{}` |  |
| prometheus.enable | bool | `false` |  |
| rbac.enable | bool | `true` |  |
| tolerations | list | `[]` |  |

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
