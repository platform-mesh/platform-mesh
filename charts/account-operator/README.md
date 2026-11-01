# account-operator

A Helm chart to deploy platform-mesh Account-Operator

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
| controllers.accountInfo.enabled | bool | `true` | Enable AccountInfo controller |
| deployment.replicas | int | `1` |  |
| deployment.specTemplate.annotations | object | `{}` | The annotations for the deployment |
| deployment.specTemplate.labels | object | `{}` | The labels for the deployment |
| hostAliases.enabled | bool | `false` |  |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/account-operator"` | The image repository path (without registry) |
| istio.enabled | bool | `false` |  |
| kcp | object | `{"apiExportEndpointSliceName":"core.platform-mesh.io"}` | The kcp configuration |
| kcp.apiExportEndpointSliceName | string | `"core.platform-mesh.io"` | kcp APIExportEndpointSliceName which is used for reconciliation |
| kubeconfigSecret | string | `"account-operator-kubeconfig"` | Name of the Secret holding the kubeconfig used to reach the API server. This Secret is not created by this Helm chart and is not meant to be created by the user: in a regular Platform Mesh installation it is created and managed by the PM Operator, which provisions a `<component>-kubeconfig` Secret for each PM component. |
| operator.leaderElect | bool | `true` |  |
| security.mountServiceAccountToken | bool | `true` | Mount the service account token |
| subroutines.accountInfo.enabled | bool | `true` | Enable AccountInfo subroutine |
| subroutines.workspace.enabled | bool | `true` | Enable workspace subroutine |
| subroutines.workspaceReady.enabled | bool | `true` | Enable workspace ready subroutine |
| subroutines.workspaceType.enabled | bool | `true` | Enable workspace type subroutine |
| webhooks.additionalAccountTypes[0] | string | `"account"` |  |
| webhooks.caDuration | string | `"2160h"` | CA certificate duration (default: 3 months) |
| webhooks.caRenewBefore | string | `"720h"` | CA certificate renewal time before expiration (default: 30 days) |
| webhooks.certDir | string | `"/certs"` | The directory for webhook certificates |
| webhooks.certDuration | string | `"2160h"` | Serving certificate duration (default: 3 months) |
| webhooks.certRenewBefore | string | `"720h"` | Serving certificate renewal time before expiration (default: 30 days) |
| webhooks.denyList | list | `["admin","root","system","www"]` | List of denied organization names |
| webhooks.enabled | bool | `true` | Enable webhooks |
| webhooks.register | bool | `false` | Register webhooks, flag to toggle if webhooks should be registered on the runtime cluster |

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
