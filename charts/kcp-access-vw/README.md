# kcp-access-vw

kcp Access Virtual Workspace — serves SelfClusterAccessReview (permission-aware workspace discovery) behind kcp's front-proxy. Renders kcp-operator VirtualWorkspace and Kubeconfig resources (kcp-operator v0.9.0 or newer required).

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Requirements

| Repository | Name | Description | Sources |
|------------|------|-------------|---------|
| `file://../common` | `common` | The `common` chart is a library of common resources that are shared across all other charts in the repository. It has no templates, but provides helm template functions and [default values](https://github.com/platform-mesh/platform-mesh/blob/main/charts/common/values.yaml) that can be used by other charts. |[source](https://github.com/platform-mesh/platform-mesh/tree/main/charts/common)|

## kcp prerequisites

The chart renders [kcp-operator](https://github.com/kcp-dev/kcp-operator)
`VirtualWorkspace` and `Kubeconfig` resources (operator v0.9.0 or newer) in
the release namespace; it deploys no workloads itself. The referenced
RootShard and FrontProxy must exist, and the front-proxy needs an
`additionalPathMappings` entry routing `/services/access` to the virtual
workspace service.

An init container bootstraps kcp on every pod start (idempotent): it creates
the workspace tree, installs the APIResourceSchema, APIExport, bind RBAC and
APIExportEndpointSlice, and grants the server identity the controller role.

## Installing on top of Platform Mesh

The chart is not part of the Platform Mesh default profile; it is an optional
add-on for a running installation:

```sh
helm install kcp-access-vw oci://ghcr.io/platform-mesh/helm-charts/kcp-access-vw \
  -n platform-mesh-system \
  --set external.hostname=kcp.api.portal.localhost \
  --set external.port=8443
```

Route the endpoint through the front-proxy by extending the `infra` chart's
`kcp.frontProxy.additionalPathMappings` (lists replace on override, so repeat
the existing entries) with:

```yaml
- path: /services/access
  backend: https://access-vw-virtual-workspace.platform-mesh-system.svc.cluster.local:6443
  backend_server_ca: /etc/kcp/tls/ca/tls.crt
  proxy_client_cert: /etc/kcp-front-proxy/requestheader-client/tls.crt
  proxy_client_key: /etc/kcp-front-proxy/requestheader-client/tls.key
```

For callers authenticating with bearer tokens, the front-proxy also needs
OIDC enabled against the installation's Keycloak (`kcp.auth.oidc` in the
`infra` chart). In the local-setup kind cluster, add a `hostAliases` entry
pointing `kcp.api.portal.localhost` at the traefik ClusterIP so the minted
kubeconfigs resolve from inside pods.
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| controllersWorkspace | string | `"controllers"` | Workspace under workspacePrefix holding the APIExport. |
| external.hostname | string | `"kcp.example.com"` | External hostname of the kcp front-proxy |
| external.port | int | `6443` | External port of the kcp front-proxy |
| hostAliases | list | `[]` | Extra /etc/hosts entries for the pod, e.g. when the front-proxy hostname only resolves via cluster-external DNS. |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"kcp-dev/contrib-virtual-workspaces/access-vw"` | The image repository path (without registry) |
| image.tag | string | `""` | The image tag (defaults to appVersion) |
| kcp.frontProxy | string | `"frontproxy"` | FrontProxy the Kubeconfig CRs target. Bootstrapping walks workspace paths, which only resolve through the front-proxy. |
| kcp.rootShard | string | `"root"` | RootShard the VirtualWorkspace connects to. |
| replicas | int | `1` | The graph is held in memory; keep a single replica. |
| resources.limits.memory | string | `"512Mi"` | Memory limit |
| resources.requests.cpu | string | `"100m"` | CPU request |
| resources.requests.memory | string | `"128Mi"` | Memory request |
| server.apiExportEndpointSlice | string | `"access.contrib.kcp.io"` | Name of the APIExportEndpointSlice for the access VW's APIExport. |
| server.endpointBase | string | `""` | Base URL for per-cluster endpoints returned in SCAR responses; defaults to https://<external.hostname>:<external.port>/clusters/. |
| workspacePrefix | string | `"root:access"` | Bootstrap (init container): workspace prefix for the access APIExport tree. |

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
