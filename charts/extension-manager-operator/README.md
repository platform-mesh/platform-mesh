# extension-manager-operator

A Helm chart for extension-manager-operator which manages resources like ContentConfigurations and exposes REST `/validate` endpoint

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)

## Requirements

| Repository | Name | Description | Sources |
|------------|------|-------------|---------|
| `oci://ghcr.io/platform-mesh/helm-charts` | `common` | The `common` chart is a library of common resources that are shared across all other charts in the repository. It has no templates, but provides helm template functions and [default values](https://github.com/platform-mesh/helm-charts/blob/main/charts/common/values.yaml) that can be used by other charts. |[source](https://github.com/platform-mesh/helm-charts/tree/main/charts/common)|
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| clusterScopedName.prefix | string | `""` | Prefix for the names of cluster-scoped resources (ClusterRoles, ClusterRoleBindings, webhook configurations), so several installations can share a cluster. A global.clusterScopedName value takes precedence over this one |
| clusterScopedName.suffix | string | `""` | Suffix for the names of cluster-scoped resources |
| commonAnnotations | object | `{}` |  |
| entityTypeValidation.enabled | bool | `false` | enable entityType validation for ContentConfigurations |
| gatewayApi.enabled | bool | `false` | Toggle to enable/disable Gateway API resources |
| gatewayApi.httpRoute.filters | list | `[]` | list of HTTPRoute filters (default: none) |
| gatewayApi.main | object | `{"gateway":{"port":8443}}` | HTTPS Terminate configuration |
| gatewayApi.name | string | `"k8sapi-gateway"` | Name of the Gateway resource |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/extension-manager-operator"` | The image repository path (without registry) |
| istio.enabled | bool | `false` | enable Istio VirtualService |
| kcp.apiExportEndpointSliceName | string | `"ui.platform-mesh.io"` |  |
| kcp.kubeconfig.secret | string | `"extension-manager-operator-kubeconfig"` |  |
| kubeconfigSecret | string | `""` |  |
| pathPrefix | string | `"/validate"` | Path prefix for routing rules in Gateway API and Istio VirtualService |
| validationServer.host | string | `"*"` | host for the validation VirtualService |
| validationServer.port | int | `8088` | port for the validation server |

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
