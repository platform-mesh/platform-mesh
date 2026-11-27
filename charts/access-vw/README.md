# access-vw

kcp Access Virtual Workspace — serves SelfClusterAccessReview (which workspaces can the caller access) behind kcp's front-proxy at /services/access, answered from OpenFGA or from kcp RBAC. Renders kcp-operator VirtualWorkspace and Kubeconfig resources (kcp-operator v0.9.0 or newer required).

![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)
## Requirements

| Repository | Name | Description | Sources |
|------------|------|-------------|---------|
| `file://../common` | `common` | The `common` chart is a library of common resources that are shared across all other charts in the repository. It has no templates, but provides helm template functions and [default values](https://github.com/platform-mesh/platform-mesh/blob/main/charts/common/values.yaml) that can be used by other charts. |[source](https://github.com/platform-mesh/platform-mesh/tree/main/charts/common)|

## What it serves

The access virtual workspace
([operators/account-operator/virtual-workspace](https://github.com/platform-mesh/platform-mesh/tree/main/operators/account-operator/virtual-workspace))
answers a `SelfClusterAccessReview`: which workspaces can the caller access.
It is part of the Platform Mesh profiles.

With `provider: openfga` (the default) the answer comes from OpenFGA. A review
is addressed to a workspace of an organization and lists the workspaces of the
accounts of that organization the caller can access:

```sh
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{}' \
  https://kcp.api.<base domain>/services/access/clusters/root:orgs:<organization>/apis/access.platform-mesh.io/v1alpha1/selfclusteraccessreviews
```

With `provider: rbac` the answer comes from the RBAC bindings of the
workspaces that bind the `access.platform-mesh.io` APIExport; an init
container installs that export on every pod start (idempotent).

`provider: rbac,openfga` runs both and merges their answers; the chart then
renders what each of them needs.

## Through the GraphQL gateway

Browsers do not call kcp directly. The chart registers the virtual workspace
with the GraphQL gateway (`clusterAccess.enabled`, a `ClusterAccess` named
`access`), which serves it on the portal's origin as the endpoint
`single-access`. The workspace a review is addressed to goes into the
`clusterTarget` extension of the request, and the gateway insists on a name
for the object:

```sh
curl -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  https://<portal>/gateway/api/clusters/single-access/graphql -d '{
    "query": "mutation { access_platform_mesh_io { v1alpha1 { createSelfClusterAccessReview(object: {metadata: {name: \"review\"}}) { status { clusters { clusterName account { name path displayName type ready } } } } } } }",
    "extensions": {"clusterTarget": "root:orgs:<organization>"}
  }'
```

The gateway validates the token against kcp for that workspace and forwards
it. The kubeconfig Secret the `ClusterAccess` refers to
(`access-vw-clusteraccess-kubeconfig`, created by platform-mesh-operator) is
used for discovery only.

## kcp prerequisites

The chart renders [kcp-operator](https://github.com/kcp-dev/kcp-operator)
`VirtualWorkspace` and `Kubeconfig` resources (operator v0.9.0 or newer) in
the release namespace; it deploys no workloads itself. The referenced
RootShard and FrontProxy must exist.

The front-proxy routes `/services/access/clusters/{cluster}` and
`/services/access` to the virtual workspace; both are in the default
`kcp.frontProxy.additionalPathMappings` of the `infra` chart. The `{cluster}`
placeholder lets the front-proxy authenticate a caller with the
authenticators of the workspace a review is addressed to, which is what
accepts tokens of an organization's own identity provider.

The server reads kcp with the identity of the `access-vw-server` Kubeconfig.
With `provider: openfga` it has to get Stores in the organizations workspace
and LogicalClusters and Workspaces in every workspace of an organization, and
carries the groups of `openfga.serverGroups` for that.

The kubeconfigs address the front-proxy by its external hostname, which has to
resolve from inside the pod; where it does not, set `hostAliases`.
## Values
| Key | Type | Default | Description |
|-----|------|---------|-------------|
| clusterAccess.auth.kubeconfigSecretRef.key | string | `"kubeconfig"` |  |
| clusterAccess.auth.kubeconfigSecretRef.name | string | `"access-vw-clusteraccess-kubeconfig"` | Secret with a kubeconfig for /services/access of the kcp front proxy; platform-mesh-operator creates it. |
| clusterAccess.auth.kubeconfigSecretRef.namespace | string | `""` | Namespace of the Secret; defaults to the release namespace. |
| clusterAccess.enabled | bool | `true` | Register the virtual workspace with the GraphQL gateway. Needs the ClusterAccess CRD of the gateway. |
| clusterAccess.name | string | `"access"` | Name of the ClusterAccess. The gateway serves it at /api/clusters/<provider>-<name>/graphql, single-access in a Platform Mesh installation. |
| clusterAccess.path | string | `""` | Name in the gateway URL, if it should differ from the name. |
| controllersWorkspace | string | `"controllers"` | Workspace under workspacePrefix holding the APIExport (provider rbac). |
| exposure.baseDomain | string | `""` | Public hostname without protocol or port; the kcp front-proxy is at kcp.api.<baseDomain>. |
| exposure.port | int | `443` | Public port. |
| external.hostname | string | `""` | External hostname of the kcp front-proxy; defaults to kcp.api.<exposure.baseDomain> |
| external.port | string | `nil` | External port of the kcp front-proxy; defaults to exposure.port |
| hostAliases.enabled | bool | `false` | Add the entries to the pod |
| hostAliases.entries | list | `[]` | Entries, each with `ip` and `hostnames` |
| image.registry | string | `"ghcr.io"` | The image registry |
| image.repository | string | `"platform-mesh/platform-mesh/access-virtual-workspaces"` | The image repository path (without registry) |
| image.tag | string | `""` | The image tag (defaults to appVersion) |
| kcp.frontProxy | string | `"frontproxy"` | FrontProxy the Kubeconfig CRs target. Bootstrapping walks workspace paths, which only resolve through the front-proxy. |
| kcp.rootShard | string | `"root"` | RootShard the VirtualWorkspace connects to. |
| openfga.addr | string | `""` | gRPC address of OpenFGA, host:port; defaults to the `openfga` Service of the release namespace, port 8081. |
| openfga.objectType | string | `"core_platform-mesh_io_account"` | OpenFGA type of an account. |
| openfga.orgsWorkspace | string | `"root:orgs"` | Workspace holding the organizations, each with an OpenFGA store named by the Store object of the organization's name in it. |
| openfga.relation | string | `"get"` | Relation a user needs on an account for its workspace to be listed. |
| openfga.serverGroups | list | `["system:kcp:admin"]` | Groups of the identity the server reads kcp with. It has to get Stores in orgsWorkspace, and LogicalClusters and Workspaces in every workspace of an organization. |
| provider | string | `"openfga"` | Where the answer of a review comes from. `openfga` asks OpenFGA per review and does not look at RBAC; a review has to be addressed to a workspace of an organization, /services/access/clusters/<workspace>/... `rbac` indexes the RBAC bindings of the workspaces that bind the access APIExport. `rbac,openfga` merges both: a workspace either of them lists is listed. |
| replicas | int | `1` | The graph is held in memory; keep a single replica. |
| resources.limits.memory | string | `"512Mi"` | Memory limit |
| resources.requests.cpu | string | `"100m"` | CPU request |
| resources.requests.memory | string | `"128Mi"` | Memory request |
| server.apiExportEndpointSlice | string | `"access.platform-mesh.io"` | Name of the APIExportEndpointSlice for the access VW's APIExport (provider rbac). |
| server.endpointBase | string | `""` | Base URL for per-cluster endpoints returned in SCAR responses; defaults to https://<external.hostname>:<external.port>/clusters/. |
| workspacePrefix | string | `"root:access"` | Bootstrap (init container, provider rbac): workspace prefix for the access APIExport tree. |

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
