# backup-operator

A Helm chart to deploy the platform-mesh backup-operator. It backs up the kcp etcd and the kcp certificates, the restore command of the same binary restores them.

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/backup-operator"` | The image repository path (without registry) |
| image.tag | string | `""` | Image tag; defaults to the chart appVersion when empty |
| leaderElect | bool | `true` | Enable leader election |
| log.level | string | `"info"` | log level. Permissible values: debug, info, warn, error |
| resources.limits.memory | string | `"256Mi"` |  |
| resources.requests.cpu | string | `"20m"` |  |
| resources.requests.memory | string | `"64Mi"` |  |

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
# backup-operator

![Version: 0.0.0](https://img.shields.io/badge/Version-0.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.0.0](https://img.shields.io/badge/AppVersion-0.0.0-informational?style=flat-square)

A Helm chart to deploy the platform-mesh backup-operator. It backs up the kcp etcd and the kcp certificates, the restore command of the same binary restores them.

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| file://../common | common | 0.0.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/backup-operator"` | The image repository path (without registry) |
| image.tag | string | `""` | Image tag; defaults to the chart appVersion when empty |
| leaderElect | bool | `true` | Enable leader election |
| log.level | string | `"info"` | log level. Permissible values: debug, info, warn, error |
| resources.limits.memory | string | `"256Mi"` |  |
| resources.requests.cpu | string | `"20m"` |  |
| resources.requests.memory | string | `"64Mi"` |  |

