# common

A Helm chart containing reuse templates

![Type: library](https://img.shields.io/badge/Type-library-informational?style=flat-square)
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| defaults.certManager.enabled | bool | `false` | toggle to enable/disable cert-manager |
| defaults.deployment.maxSurge | int | `5` | maxSurge |
| defaults.deployment.maxUnavailable | int | `0` | maxUnavailable |
| defaults.deployment.resources.limits | object | `{"cpu":"","memory":"512Mi"}` | cpu and memory limits for the deployment |
| defaults.deployment.resources.requests | object | `{"cpu":"40m","memory":"50Mi"}` | cpu and memory requests for the deployment |
| defaults.deployment.revisionHistoryLimit | int | `3` | deployment revision history limit |
| defaults.deployment.strategy | string | `"RollingUpdate"` | deployment strategy |
| defaults.enableHttp2 | bool | `true` | enable HTTP/2 for metrics and webhook servers |
| defaults.environment | string | `""` | default environment, this value is primarily used for observability, e.g. logs. Permissible values: any string (e.g. production, staging, development) |
| defaults.externalSecrets.enabled | bool | `false` | toggle to enable/disable external-secrets |
| defaults.externalSecrets.secretStore.kind | string | `"SecretStore"` | the default kind to be used in external secrets |
| defaults.externalSecrets.secretStore.name | string | `"environment-store"` | the default store name to be used in external secrets |
| defaults.fga.enabled | bool | `true` | toggle to enable/disable experimental FGA features |
| defaults.health.liveness | object | `{"failureThreshold":1,"path":"/healthz"}` | liveness probe parameters |
| defaults.health.periodSeconds | int | `10` | health period |
| defaults.health.port | int | `8090` | health port |
| defaults.health.readiness | object | `{"initialDelaySeconds":5,"path":"/readyz","periodSeconds":10}` | readiness probe parameters |
| defaults.health.startup | object | `{"failureThreshold":30,"path":"/readyz"}` | startup probe parameters |
| defaults.hostAliases.enabled | bool | `false` | toggle to enable/disable hostAliases configuration |
| defaults.hostAliases.entries | list | `[]` | host aliases |
| defaults.imagePullPolicy | string | `"IfNotPresent"` | imagePullPolicy is the policy to use when pulling images for all charts |
| defaults.istio.enabled | bool | `false` | toggle to enable/disable istio |
| defaults.istio.gateway.name | string | `"gateway"` | name of the gateway |
| defaults.log.level | string | `"warn"` | default log level |
| defaults.metrics.port | int | `9090` | metrics port |
| defaults.operator.leaderElect | bool | `true` | by default operators participate in leader election |
| defaults.operator.maxConcurrentReconciles | int | `10` | number of concurrent reconciles per controller |
| defaults.operator.shutdownTimeout | string | `"1m"` | duration on how long the operator waits before shutting down |
| defaults.port | int | `8080` | service port |
| defaults.region | string | `""` | default region, this value is primarily used for observability, e.g. logs. Permissible values: any string (e.g. us-east-1, eu-west-1) |
| defaults.securityContext.fsGroup | int | `2000` | fsGroup id to run the container |
| defaults.securityContext.runAsGroup | int | `3000` | group id to run the container |
| defaults.securityContext.runAsUser | int | `1000` | user id to run the container |
| defaults.sentry.enabled | bool | `false` | toggle to enable/disable sentry integration |
| defaults.sentry.externalSecrets.secretKey | string | `"sentry/sentry-dsn"` | the secret name that holds the sentry DSNs |
| defaults.service.port | int | `8080` |  |
| defaults.service.type | string | `"ClusterIP"` |  |
| defaults.tracing.collector.endpoint | string | `"observability-opentelemetry-collector.platform-mesh-observability.svc.cluster.local:4317"` | the OpenTelemetry collector endpoint |
| defaults.tracing.enabled | bool | `false` | toggle to enable/disable OpenTelemetry |

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
