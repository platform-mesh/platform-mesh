# rebac-authz-webhook

A Helm chart for Kubernetes

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| certManager.caSecretName | string | `"rebac-authz-webhook-webhook-ca"` |  |
| certManager.dnsNames | list | `[]` |  |
| certManager.enabled | bool | `true` |  |
| certManager.ipAddresses | list | `[]` |  |
| clusterScopedName.prefix | string | `""` | Prefix for the names of cluster-scoped resources (ClusterRoles, ClusterRoleBindings, webhook configurations), so several installations can share a cluster. A global.clusterScopedName value takes precedence over this one |
| clusterScopedName.suffix | string | `""` | Suffix for the names of cluster-scoped resources |
| deployment.resources.limits.cpu | string | `"500m"` |  |
| deployment.resources.limits.memory | string | `"128Mi"` |  |
| deployment.resources.requests.cpu | string | `"40m"` |  |
| deployment.resources.requests.memory | string | `"50Mi"` |  |
| health.port | int | `8081` |  |
| healthProbeBindAddress | string | `":8081"` |  |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/rebac-authz-webhook"` | The image repository path (without registry) |
| image.tag | string | `""` |  |
| istio.dnsNames | list | `[]` |  |
| istio.enabled | bool | `false` |  |
| kcp.apiExportEndpointSliceName | string | `""` | kcp APIExportEndpointSliceName to watch. Empty string means auto-discover all slices |
| kcp.kubeconfig.secret | string | `"rebac-authz-webhook-kubeconfig"` |  |
| openfga.endpoint | string | `"openfga.platform-mesh-system.svc.cluster.local:8081"` |  |
| service.annotations | object | `{}` |  |
| service.clusterIP | string | `""` |  |
| service.metricsPort | int | `8080` |  |
| service.port | int | `9443` |  |
| service.type | string | `"ClusterIP"` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.automount | bool | `true` |  |
| serviceAccount.create | bool | `true` |  |
| webhook.cacheMissCleanupInterval | string | `"2m"` |  |
| webhook.cacheMissMaxRetries | int | `1` |  |
| webhook.cacheMissRetryAfter | string | `"1s"` |  |
| webhook.cacheMissTTL | string | `"5m"` |  |

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
