# marketplace

A Helm chart to deploy the Platform Mesh marketplace virtual workspace, which serves MarketplaceEntry resources behind the kcp front-proxy at /services/marketplace

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)

## Requirements

| Repository | Name | Description | Sources |
|------------|------|-------------|---------|
| `file://../common` | `common` | The `common` chart is a library of common resources that are shared across all other charts in the repository. It has no templates, but provides helm template functions and [default values](https://github.com/platform-mesh/platform-mesh/blob/main/charts/common/values.yaml) that can be used by other charts. |[source](https://github.com/platform-mesh/platform-mesh/tree/main/charts/common)|
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| authenticationKubeconfigSecretName | string | `"portal-kubeconfig"` |  |
| cert.duration | string | `"2160h0m0s"` | Certificate duration |
| cert.extraDnsNames | list | `["localhost"]` | Extra DNS names for the certificate |
| cert.issuer.kind | string | `"Issuer"` | Issuer reference |
| cert.issuer.name | string | `"root-server-ca"` | Issuer name |
| cert.key.algorithm | string | `"RSA"` | Key algorithm (e.g. RSA or ECDSA) |
| cert.key.size | int | `4096` | Key size (e.g. 2048, 3072, 4096 for RSA or 256, 384, 521 for ECDSA) |
| cert.renewBefore | string | `"168h0m0s"` | Certificate renew before |
| cert.secretName | string | `"marketplace-cert"` | Secret name to store the certificate |
| clientCASecretName | string | `"frontproxy-merged-client-ca"` |  |
| clusterAccess.marketplace.auth | object | `{"kubeconfigSecretRef":{"key":"kubeconfig","name":"virtual-workspace-clusteraccess-kubeconfig"}}` | Path for gateway url path: "" |
| clusterAccess.marketplace.enabled | bool | `true` |  |
| deployment.contentForLabel | string | `"ui.platform-mesh.io/content-for"` | Label on an APIExport naming the ProviderMetadata it belongs to. |
| deployment.resourceSchemaExportName | string | `"ui.platform-mesh.io"` | APIExportEndpointSlice through which ProviderMetadata and APIExports of all providers are read. |
| hostAliases.enabled | bool | `false` |  |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/marketplace"` | The image repository path (without registry) |
| kcp.internalFrontProxyUrl | string | `""` | Required kcp front-proxy root URL supplied by the operator; used for cluster-aware clients. |
| kubeconfigSecretName | string | `"account-operator-kubeconfig"` |  |
| requestHeaderClientCASecretName | string | `"root-requestheader-client-ca"` |  |
| service.port | int | `8443` |  |

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
