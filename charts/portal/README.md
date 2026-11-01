# portal

Helm Chart for the Platform Mesh Portal

## Requirements

| Repository | Name | Description | Sources |
|------------|------|-------------|---------|
| `file://../common` | `common` | The `common` chart is a library of common resources that are shared across all other charts in the repository. It has no templates, but provides helm template functions and [default values](https://github.com/openmfp/helm-charts/blob/main/charts/common/values.yaml) that can be used by other charts. |[source](https://github.com/openmfp/helm-charts/tree/main/charts/common)|
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| auth.default.baseDomain | string | `""` | baseDomain used by the portal |
| auth.default.clientId | string | `""` | client id |
| auth.default.discoveryUrl | string | `""` | discovery url used for the idp |
| cookieDomain | string | `""` | cookie domain for auth session cookies. Must match the base domain of the portal deployment (e.g. example.com). Required. |
| developmentLandcsape | string | `"false"` | development landscape toggle. Set to "true" only in local/dev environments to enable dev-only UI features. Permissible values: "true", "false" |
| environment | string | `""` | environment label propagated to observability (e.g. logs, traces). Permissible values: any string (e.g. production, staging, development) |
| featureToggles | string | `"enableSessionAutoRefresh=true"` |  |
| frontendPort | int | `8000` | frontend port |
| gatewayApi.enabled | bool | `true` | toggle to enable the Gateway API |
| gatewayApi.httpRoute.filters | list | `[]` | list of HTTPRoute filters (default: none) |
| gatewayApi.httpRoute.hostnames | list | `[]` | hostnames for the HTTPRoute. When empty, the route matches all hostnames. |
| gatewayApi.httpRoute.parentRefs[0].name | string | `"k8sapi-gateway"` |  |
| gatewayApi.httpRoute.pathPrefix | string | `"/"` |  |
| health.port | int | `8080` | health port to be used by probes |
| hostAliases | object | `{"enabled":false,"entries":[]}` | hostAliases configuration |
| hostAliases.enabled | bool | `false` | enable hostAliases |
| hostAliases.entries | list | `[]` | hostAliases entries |
| http.protocol | string | `""` | protocol used by the portal backend. Must be set. Permissible values: http, https |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.pullPolicyOverride | string | `"IfNotPresent"` |  |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/portal"` | The image repository path (without registry) |
| initContainers | list | `[]` | init containers to run before the main container |
| kcp.kubeconfigSecret | string | `""` |  |
| kcp.url | string | `""` | kcp API URL (e.g. https://localhost:8443 for local setup) |
| kubeconfigSecret | string | `""` | allows the configuration of a kubeconfig secret for external api servers |
| uiOptions | string | `"enableFeatureToggleSetting"` |  |
| validWebcomponentUrls | string | `".?"` |  |
| validation.path | string | `"/validate"` | Path of the extension-manager validation webhook |
| validation.port | int | `8088` | Port of the extension-manager validation webhook |
| validation.protocol | string | `"http"` | Protocol used to reach the extension-manager validation webhook. Permissible values: http, https |
| validation.service | string | `"extension-manager-operator-server"` | Service name of the extension-manager validation webhook |

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
