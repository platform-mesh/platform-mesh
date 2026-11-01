# kcp-mcp-vw

kcp MCP Virtual Workspace — an MCP (Model Context Protocol) server exposing permission-scoped kcp workspaces, served behind kcp's front-proxy. Scopes each session via the Access VW (SCAR) and acts on resources via impersonation, so kcp authorizes each request as the caller. Renders kcp-operator VirtualWorkspace and Kubeconfig resources (kcp-operator v0.9.0 or newer required).

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
`additionalPathMappings` entry routing `/services/mcp` to the virtual
workspace service.

The server scopes each MCP session to the workspaces the caller can access,
resolved through the access virtual workspace (`kcp-access-vw` chart) at
`--access-url`, and performs all resource operations via impersonation, so
kcp authorizes every request as the caller. The
`mcp-virtual-workspace-impersonator` ClusterRole from the upstream
`mcp/deploy/kcp/rbac.yaml` must be applied in every workspace whose
resources the server should reach on a caller's behalf.

## OAuth client discovery

With `oidc.issuerURL` set, the server validates bearer tokens itself, which
lets MCP clients reach it directly (e.g. through a dedicated gateway
hostname) instead of through the front-proxy. Setting
`oauth.authorizationServers` additionally enables RFC 9728
protected-resource metadata and `WWW-Authenticate` hints, so clients
discover the authorization server and open a browser login on their own.
The discovery endpoints must be reachable anonymously, which the
front-proxy does not allow — route them to the service directly.

## Installing on top of Platform Mesh

The chart is not part of the Platform Mesh default profile; it is an optional
add-on for a running installation, alongside `kcp-access-vw`:

```sh
helm install kcp-mcp-vw oci://ghcr.io/platform-mesh/helm-charts/kcp-mcp-vw \
  -n platform-mesh-system \
  --set external.hostname=kcp.api.portal.localhost \
  --set external.port=8443 \
  --set oidc.issuerURL=https://portal.localhost:8443/keycloak/realms/<realm> \
  --set oidc.clientID=<client> \
  --set oidc.caSecretName=<secret with the issuer CA>
```

Additionally:

- extend the `infra` chart's `kcp.frontProxy.additionalPathMappings` with a
  `/services/mcp` route to
  `https://mcp-vw-virtual-workspace.platform-mesh-system.svc.cluster.local:6443`
  (same CA and proxy client certificate paths as the existing entries)
- enable OIDC on the front-proxy (`kcp.auth.oidc` in the `infra` chart) so
  the workspace endpoints returned by the access virtual workspace accept
  the caller's bearer token
- apply the impersonator RBAC in every workspace the server should reach

The [mcp-integration](https://github.com/platform-mesh/mcp-integration)
repository carries a scripted end-to-end setup for the local-setup kind
cluster, including Keycloak client registration and RBAC seeding.
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| access.cacheTTL | string | `"30s"` | How long a caller's workspace list is cached per session. |
| access.url | string | `""` | SCAR endpoint. Defaults to https://<external.hostname>:<external.port>/services/access. |
| external.hostname | string | `"kcp.example.com"` | External hostname of the kcp front-proxy |
| external.port | int | `6443` | External port of the kcp front-proxy |
| hostAliases | list | `[]` | Extra /etc/hosts entries for the pod, e.g. when the front-proxy hostname only resolves via cluster-external DNS. |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"kcp-dev/contrib-virtual-workspaces/mcp-vw"` | The image repository path (without registry) |
| image.tag | string | `""` | The image tag (defaults to appVersion) |
| kcp.frontProxy | string | `"frontproxy"` | FrontProxy the Kubeconfig CR targets. |
| kcp.rootShard | string | `"root"` | RootShard the VirtualWorkspace connects to. |
| oauth.authorizationServers | list | `[]` | Authorization server issuer URLs; non-empty enables discovery. |
| oauth.resource | string | `""` | Canonical resource identifier; empty derives it from each request's Host header. |
| oauth.scopes | list | `[]` | Scopes advertised in the protected-resource metadata. |
| oidc.caSecretName | string | `""` | Secret in the release namespace holding the issuer CA under key ca.crt; empty means the issuer certificate must chain to a system root. |
| oidc.clientID | string | `""` | Expected audience (client ID) of accepted tokens. |
| oidc.issuerURL | string | `""` | OIDC issuer URL; empty disables native bearer-token validation. |
| oidc.usernameClaim | string | `"email"` | Token claim used as the username. |
| oidc.usernamePrefix | string | `"oidc:"` | Prefix applied to usernames from tokens. |
| replicas | int | `1` | Number of server replicas |
| resources.limits.memory | string | `"512Mi"` | Memory limit |
| resources.requests.cpu | string | `"100m"` | CPU request |
| resources.requests.memory | string | `"128Mi"` | Memory request |
| toolsets | list | `[]` | Toolsets the server exposes (--toolsets). Empty means the server default (core, read-only). Valid: core, apis, admin, write. |

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
