# platform-mesh-operator

A Helm chart to automate bootstrapping of new environment

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Profile lifecycle

With `ocm.enabled=true`, Helm creates `platform-mesh-profile` once during install,
if the ConfigMap does not already exist. Upgrades do not create or update it, even
when `ocm.profile`, gateway values, or the bundled profile contents change. Maintain
the profile explicitly in your own configuration or GitOps source after installation.

The profile has `helm.sh/resource-policy: keep`, so it survives Helm upgrades and
uninstall. Remove it explicitly when decommissioning the installation. Keep
`ocm.enabled=true` on upgrades if this release manages the OCM and PlatformMesh
resources; the install-only behavior applies specifically to the profile.

Before upgrading a release that already manages the profile without this
annotation, preserve it once:

```shell
kubectl annotate configmap platform-mesh-profile -n platform-mesh-system \
  helm.sh/resource-policy=keep --overwrite
```

The chart blocks an upgrade of an unprotected profile owned by the same release.
This check uses Helm's live cluster lookup; use `--dry-run=server` when previewing
an upgrade. Offline `helm template` cannot inspect existing resources. Tools that
repeatedly apply install-mode renders must manage the profile separately.
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| certificateSecretName | string | `"domain-certificate"` | TLS Secret containing tls.crt, tls.key, and ca.crt. Its ca.crt is trusted by the operator. |
| certificates | object | `{"selfSigned":{"caSecretName":"domain-certificate-ca","enabled":false}}` | Certificate issuance configuration. Requires cert-manager when selfSigned.enabled=true. |
| certificates.selfSigned | object | `{"caSecretName":"domain-certificate-ca","enabled":false}` | Create a private self-signed CA and use it to issue the gateway certificate. Keep disabled when the TLS Secret is managed externally. |
| certificates.selfSigned.caSecretName | string | `"domain-certificate-ca"` | Secret containing the generated private CA certificate and key. |
| clusterScopedName.prefix | string | `""` | Prefix for the names of cluster-scoped resources (ClusterRoles, ClusterRoleBindings, webhook configurations), so several installations can share a cluster. A global.clusterScopedName value takes precedence over this one |
| clusterScopedName.suffix | string | `""` | Suffix for the names of cluster-scoped resources |
| crds.enabled | bool | `true` |  |
| deployment.replicas | int | `1` |  |
| extraArgs[0] | string | `"--subroutines-feature-toggles-enabled=true"` |  |
| gateway | object | `{"baseDomain":"","className":"","clusterIP":"","port":8443}` | Gateway/exposure configuration (used when ocm.enabled=true). |
| gateway.baseDomain | string | `""` | Public DNS name used for Platform Mesh endpoints. |
| gateway.className | string | `""` | Name of the GatewayClass the Platform Mesh Gateway attaches to. Set this to the GatewayClass registered by your own controller (e.g. `envoy`, `istio`) to run Platform Mesh on a different Gateway API implementation. |
| gateway.clusterIP | string | `""` | ClusterIP of the gateway controller Service. Required when ocm.profile=kind, where component pods resolve the Platform Mesh hostnames to it through host aliases; production uses public DNS. |
| gateway.port | int | `8443` | External HTTPS port exposed by the Gateway. |
| idp.registrationAllowed | bool | `false` |  |
| image.digest | string | `""` | The image digest (when set, overrides tag: registry/repository@digest) |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/platform-mesh-operator"` | The image repository path (without registry) |
| istio.enabled | bool | `false` |  |
| jwt.userIdClaim | string | `"email"` |  |
| kcp | object | `{"frontProxyPort":443}` | KCP connectivity configuration (used when ocm.enabled=true). |
| kcp.frontProxyPort | int | `443` | Internal port of the KCP front-proxy Service. Production Services expose 443; the kind profile uses 8443. |
| log.level | string | `"info"` | log level. Permissible values: debug, info, warn, error |
| ocm | object | `{"enabled":false,"installSigningCertificate":true,"profile":"production","repository":"ghcr.io/platform-mesh","services":{"account-operator":true,"cnpg-operator":false,"extension-manager-operator":true,"iam-service":true,"iam-ui":true,"infra":true,"kcp-operator":true,"keycloak-operator":false,"kubernetes-graphql-gateway":true,"marketplace-ui":true,"observability":false,"openfga":true,"portal":true,"rebac-authz-webhook":true,"security-operator":true,"terminal-controller-manager":false,"virtual-workspaces":true},"signatureName":"platform-mesh.platform-mesh","version":"0.5.2"}` | When enabled, manages the OCM Repository/Component CRs and the PlatformMesh CR, and creates the profile ConfigMap once on Helm install if absent. Helm upgrades preserve the existing profile. Disable to supply PlatformMesh resources from a separate GitOps source. |
| ocm.enabled | bool | `false` | Enable the bundled installation resources. Keep enabled on upgrades to continue managing the OCM and PlatformMesh resources. |
| ocm.installSigningCertificate | bool | `true` | Set to false when the OCM component is signed by a different trusted key. |
| ocm.profile | string | `"production"` | Initial profile, applied only on Helm install when no profile ConfigMap exists. Changes on upgrade do not update the profile. `production` assumes the infrastructure prerequisites already exist. |
| ocm.repository | string | `"ghcr.io/platform-mesh"` | OCI registry (and optional path) the Platform Mesh OCM components are read from. Point this at a mirror, or at a fork's registry, to install from there. |
| ocm.services | object | `{"account-operator":true,"cnpg-operator":false,"extension-manager-operator":true,"iam-service":true,"iam-ui":true,"infra":true,"kcp-operator":true,"keycloak-operator":false,"kubernetes-graphql-gateway":true,"marketplace-ui":true,"observability":false,"openfga":true,"portal":true,"rebac-authz-webhook":true,"security-operator":true,"terminal-controller-manager":false,"virtual-workspaces":true}` | Enable/disable the individual OCM components. The operator reconciles only the enabled components. |
| ocm.services.cnpg-operator | bool | `false` | disabled by default because cnpg operator is deployed by prerequisities helm-chart |
| ocm.services.keycloak-operator | bool | `false` | disabled by default because keycloak operator is deployed by prerequisities helm-chart |
| ocm.signatureName | string | `"platform-mesh.platform-mesh"` | Name of the OCM signature the components are verified against. |
| ocm.version | string | `"0.5.2"` | Platform Mesh OCM component version to reconcile. |
| operator.leaderElect | bool | `true` |  |
| remoteInfra.enabled | bool | `false` | Enables reconciliation of PlatformMesh resources on remote clusters |
| remoteInfra.secretKey | string | `"kubeconfig"` |  |
| remoteInfra.secretName | string | `"platform-mesh-kubeconfig"` | Name of the secret containing the kubeconfig for remote cluster access where the PlatformMesh resources will be deployed |
| remoteRuntime.enabled | bool | `false` | Enables deployment to remote clusters. Set to true if the operator is not deployed on the same cluster where the Infra artefacts will be created. |
| remoteRuntime.infra.secretKey | string | `"kubeconfig"` |  |
| remoteRuntime.infra.secretName | string | `"platform-mesh-secret"` | Name of the secret containing Runtime cluster kubeconfig |
| remoteRuntime.secretKey | string | `"kubeconfig"` |  |
| remoteRuntime.secretName | string | `"platform-mesh-secret"` | Name of the secret containing the kubeconfig for the cluster where the created Infra artefacts will be created. NOTE: target deployment will alway be same as the cluster where the Platform Mesh resource lives. |
| tracing.collector.endpoint | string | `"observability-opentelemetry-collector.observability.svc.cluster.local:4317"` |  |
| tracing.enabled | bool | `false` |  |

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
