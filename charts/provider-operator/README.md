# provider-operator

Onboards providers into Platform Mesh (providers.platform-mesh.io)

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| clusterScopedName.prefix | string | `""` | Prefix for the names of the ClusterRole and ClusterRoleBinding, so several installations can share a cluster. A global.clusterScopedName value takes precedence |
| clusterScopedName.suffix | string | `""` |  |
| extraArgs | list | `[]` |  |
| image.digest | string | `""` | When set, the image is pulled by digest instead of by tag |
| image.registry | string | `"ghcr.io"` |  |
| image.repository | string | `"platform-mesh/platform-mesh/provider-operator"` |  |
| kcp.frontProxyPort | int | `443` | Port of the kcp front-proxy Service in the release namespace: 443 in production, 8443 on kind |

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
