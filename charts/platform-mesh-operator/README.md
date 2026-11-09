# platform-mesh-operator

A Helm chart to automate bootstrapping of new environment

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Installation profiles

`production` uses public DNS and the production topology. `kind` includes the
frontend and its supporting services for local evaluation. `kind-optimized` is
a third, backend-only alternative for local Kubernetes, IAM, and authorization
experiments with a smaller resource budget.

Select `ocm.profile=kind-optimized` when installing the operator chart. Use
the optimized Helm options documented in the prerequisites chart README in both
prerequisite installation passes; they work with OCI charts without a separate
values-file download. The optimized profile is merged into the regular Kind profile
so gateway, database, certificate, and backend configuration stay consistent.

The optimized profile omits portal, IAM UI/service, marketplace UI, GraphQL gateway
and listener, extension-manager-operator, and virtual-workspaces. It retains
account-operator, security-operator, Keycloak, OpenFGA, KCP, and delivery controllers.
Cache-server and the internal root proxy run at one replica. Root KCP requests
1Gi/250m to accommodate the measured backend footprint. Keycloak keeps a 2Gi
memory limit while requesting 1Gi through its supported resource field.
The pinned kcp-operator does not support cache-server container resource overrides,
so its default request is retained for the remaining replica.

This profile is intended for backend development, not a highly available production
installation. Resource measurements and verification are recorded in
`docs/deployment/resource-savings/`.

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
| ocm | object | `{"enabled":false,"installSigningCertificate":true,"profile":"production","repository":"ghcr.io/platform-mesh","signatureName":"platform-mesh.platform-mesh","version":"0.5.2"}` | When enabled, manages the OCM Repository/Component CRs and the PlatformMesh CR, and creates the profile ConfigMap once on Helm install if absent. Helm upgrades preserve the existing profile. Disable to supply PlatformMesh resources from a separate GitOps source. |
| ocm.enabled | bool | `false` | Enable the bundled installation resources. Keep enabled on upgrades to continue managing the OCM and PlatformMesh resources. |
| ocm.installSigningCertificate | bool | `true` | Set to false when the OCM component is signed by a different trusted key. |
| ocm.profile | string | `"production"` | Initial profile, applied only on Helm install when no profile ConfigMap exists. Changes on upgrade do not update the profile. `production` assumes the infrastructure prerequisites already exist; `kind` includes the full local installation; `kind-optimized` runs the backend without frontend services. |
| ocm.repository | string | `"ghcr.io/platform-mesh"` | OCI registry (and optional path) the Platform Mesh OCM components are read from. Point this at a mirror, or at a fork's registry, to install from there. |
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
