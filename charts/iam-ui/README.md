# iam-ui

Helm Chart for the iam-ui

## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| deployment.maxSurge | int | `5` |  |
| deployment.maxUnavailable | int | `0` |  |
| exposure.hostnames | list | `[]` | hostnames to be used for exposure |
| gatewayApi.httpRoute.parentRefs[0].name | string | `"k8sapi-gateway"` |  |
| gatewayApi.httpRoute.pathPrefix | string | `"/ui/iam"` |  |
| health.port | int | `8080` |  |
| health.readiness.path | string | `"/healthz"` |  |
| health.startup.path | string | `"/healthz"` |  |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/iam-ui"` | The image repository path (without registry) |
| port | int | `8080` |  |

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
