# platform-mesh-sdk-access-controller

A Helm chart to deploy the Platform Mesh access SDK AccessIdentity controller on a user-side cluster.

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| crds.enabled | bool | `false` |  |
| deployment.replicas | int | `1` |  |
| deployment.specTemplate.annotations | object | `{}` |  |
| deployment.specTemplate.labels | object | `{}` |  |
| image.digest | string | `""` | Image digest (when set, overrides tag: registry/repository@digest). |
| image.registry | string | `"ghcr.io"` | Image registry. |
| image.repository | string | `"platform-mesh/platform-mesh/platform-mesh-sdk-access"` | Image repository (without registry). |
| operator.leaderElect | bool | `true` | Enable leader election so only one replica mints tokens at a time. Keep on in production; safe to leave on with replicas=1. |
| security.mountServiceAccountToken | bool | `true` |  |

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
# platform-mesh-sdk-access-controller

![Version: 0.0.0](https://img.shields.io/badge/Version-0.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.0.0](https://img.shields.io/badge/AppVersion-0.0.0-informational?style=flat-square)

A Helm chart to deploy the Platform Mesh access SDK AccessIdentity controller on a user-side cluster.

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| file://../common | common | 0.0.0 |
| file://../platform-mesh-sdk-access-crds | platform-mesh-sdk-access-crds | 0.0.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| crds.enabled | bool | `false` |  |
| deployment.replicas | int | `1` |  |
| deployment.specTemplate.annotations | object | `{}` |  |
| deployment.specTemplate.labels | object | `{}` |  |
| image.digest | string | `""` | Image digest (when set, overrides tag: registry/repository@digest). |
| image.registry | string | `"ghcr.io"` | Image registry. |
| image.repository | string | `"platform-mesh/platform-mesh/platform-mesh-sdk-access"` | Image repository (without registry). |
| operator.leaderElect | bool | `true` | Enable leader election so only one replica mints tokens at a time. Keep on in production; safe to leave on with replicas=1. |
| security.mountServiceAccountToken | bool | `true` |  |

