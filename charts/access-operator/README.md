# access-operator

A Helm chart to deploy the Platform Mesh access-operator on the PM runtime cluster.

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| deployment.replicas | int | `1` |  |
| deployment.specTemplate.annotations | object | `{}` |  |
| deployment.specTemplate.labels | object | `{}` |  |
| image.digest | string | `""` | Image digest (when set, overrides tag: registry/repository@digest). |
| image.registry | string | `"ghcr.io"` | Image registry. |
| image.repository | string | `"platform-mesh/platform-mesh/access-operator"` | Image repository (without registry). |
| kcp.apiExportEndpointSliceName | string | `"access.platform-mesh.io"` | Name of the APIExportEndpointSlice the operator engages via its apiexport-provider. Must match the APIExport the kubeconfig above is scoped to. |
| kcp.caBundleSecret | string | `"root-server-ca"` | Secret mounted into the Pod carrying the kcp CA bundle PEM (propagated into the /access handshake body). Must exist in the Release namespace. Default matches the ServerCA Issuer Secret cert-manager writes for a RootShard named "root" — the same Secret kcp-operator reads when it builds Kubeconfig CR outputs (see kcp-operator's internal/resources/kubeconfig/secret.go). Override if your kcp install uses a different RootShard name or provides the bundle some other way. |
| kcp.caBundleSecretKey | string | `"tls.crt"` | Key inside kcp.caBundleSecret whose value is the PEM CA bundle. cert-manager stores the Issuer's CA under tls.crt. |
| kcp.externalUrl | string | `""` | Publicly-reachable kcp URL propagated into the /access handshake body for user-side clusters to use. Required; operator refuses to perform handshakes if unset. |
| kubeconfigSecret | string | `"access-operator-kubeconfig"` | Name of the Secret holding the operator's kcp kubeconfig. This Secret is not created by this chart; in a regular Platform Mesh installation it is provisioned and renewed by platform-mesh-operator's ProvidersecretSubroutine (see DefaultProviderConnections, the "access-operator-kubeconfig" entry). The Secret carries a scoped ServiceAccount token addressing the access.platform-mesh.io APIExportEndpointSlice in root:platform-mesh-system. |
| operator.leaderElect | bool | `true` |  |
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
