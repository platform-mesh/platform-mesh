# terminal-controller-manager

A Helm chart to deploy platform-mesh Terminal Controller Manager

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)

> **Note:** The terminal-controller-manager is in the process of being open sourced. The source repository may not yet be publicly available.

## Requirements

| Repository | Name | Description | Sources |
|------------|------|-------------|---------|
| `file://../common` | `common` | The `common` chart is a library of common resources that are shared across all other charts in the repository. It has no templates, but provides helm template functions and [default values](https://github.com/platform-mesh/platform-mesh/blob/main/charts/common/values.yaml) that can be used by other charts. |[source](https://github.com/platform-mesh/platform-mesh/tree/main/charts/common)|
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| deployment.replicas | int | `1` |  |
| deployment.specTemplate.annotations | object | `{}` | The annotations for the deployment |
| deployment.specTemplate.labels | object | `{}` | The labels for the deployment |
| gateway.hostnames | list | `[]` | The hostnames for HTTPRoutes |
| gateway.name | string | `"k8sapi-gateway"` | The gateway name for HTTPRoutes |
| gateway.namespace | string | `"platform-mesh-system"` | The gateway namespace |
| hostAliases.enabled | bool | `false` |  |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/terminal-controller-manager"` | The image repository path (without registry) |
| istio.enabled | bool | `false` |  |
| kcp | object | `{"apiExportEndpointSliceName":"terminal.platform-mesh.io","kubeconfigSecret":""}` | The kcp configuration |
| kcp.apiExportEndpointSliceName | string | `"terminal.platform-mesh.io"` | kcp APIExportEndpointSliceName |
| kcp.kubeconfigSecret | string | `""` | Secret containing kubeconfig for kcp connection |
| kubeconfigSecret | string | `""` | Secret containing kubeconfig for runtime cluster (optional, defaults to in-cluster config) |
| operator.leaderElect | bool | `true` |  |
| security.mountServiceAccountToken | bool | `true` | Mount the service account token |
| subroutines.httproute.enabled | bool | `true` | Enable httproute subroutine |
| subroutines.lifetime.enabled | bool | `true` | Enable lifetime subroutine |
| subroutines.pod.enabled | bool | `true` | Enable pod subroutine |
| subroutines.service.enabled | bool | `true` | Enable service subroutine |
| terminal.hostAliasIP | string | `""` | Host alias IP for local development (optional) |
| terminal.hostAliasNames | list | `[]` | Host alias names for local development (optional) |
| terminal.image.registry | string | `"ghcr.io"` | The terminal pod image registry |
| terminal.image.repository | string | `"platform-mesh/platform-mesh/terminal"` | The terminal pod image repository path (without registry) |
| terminal.image.tag | string | `""` | Override terminal image tag (defaults to appVersion) |
| terminal.lifetime | string | `"2h"` | The terminal session lifetime (Go duration format) |
| terminal.namespace | string | `"terminal-sessions"` | The namespace where terminal pods are created |

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
