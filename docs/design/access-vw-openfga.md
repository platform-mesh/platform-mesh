# Access virtual workspace backed by OpenFGA

Status: 2026-11-24. Implemented and running in the Tilt environment; not yet
merged and not yet run through the e2e suites. What is open is listed at the
end.

## Goal

The access virtual workspace
([operators/account-operator/virtual-workspace](../../operators/account-operator/virtual-workspace),
chart `access-vw`) answers a `SelfClusterAccessReview`: which workspaces can
the caller reach. The code was imported from
[kcp-dev/contrib-access-virtual-workspace], where the answer is derived from
the Kubernetes RBAC bindings of workspaces that bind its APIExport.

Platform Mesh authorizes through OpenFGA, so that is the wrong source. Here
the answer comes from OpenFGA only and Kubernetes RBAC is not consulted. The
RBAC provider is still in the binary, behind `--provider=rbac`, and both can
run together: `--provider=rbac,openfga` merges their answers.

## Design

A review is scoped to one organization. The caller names a workspace of the
organization in the URL:

```
POST /services/access/clusters/root:orgs:<org>/apis/access.platform-mesh.io/v1alpha1/selfclusteraccessreviews
```

1. **Front proxy** resolves the workspace from the URL and authenticates the
   caller with that workspace's authenticator (the organization's identity
   provider), then forwards the identity in the request headers. This needs a
   path mapping with a `{cluster}` placeholder, see
   [kcp: per-workspace authentication for virtual workspaces][kcp-vw-auth].
2. **Virtual workspace** reduces the cluster to its organization and looks up
   the organization's OpenFGA store.
3. It makes one `ListObjects` call for the caller: type
   `core_platform-mesh_io_account`, relation `get` (a flag), user formatted
   like the tuples (`user:<name>`, colons replaced by dots).
4. It maps the returned account objects to logical clusters and returns them
   in `status.clusters`. Each entry also carries the account, so that a
   client can list accounts without reading them:

   ```yaml
   status:
     clusters:
       - clusterName: 2afpvf01tlwa6el8
         endpoint: https://kcp.api.example.com/clusters/2afpvf01tlwa6el8
         account:
           name: test
           path: root:orgs:bob:test
           displayName: Test      # spec.displayName of the Account
           type: account          # spec.type: org or account
           ready: true            # condition Ready of the Account is True
   ```

The result is every account of the organization the caller can access, not
every account that exists. Access inherits downward in the model
(`get: member or get from parent`):

| Caller | Result |
|--------|--------|
| Owner or member of the organization | the organization and all accounts below it |
| Member of one sub-account | that account and its children |
| No relation in the organization | empty |

### Decisions

- **Query OpenFGA per review; do not mirror it.** The upstream provider seam
  pushes grants into an in-memory graph. For OpenFGA that would mean
  enumerating users per account and following changes in every store, and it
  would lag behind the source. Asking OpenFGA lets it resolve inheritance and
  wildcard users itself.
- **Organization in the URL.** There is one OpenFGA store per organization.
  With the organization known, a review is one call to one store. Without it,
  every review would query every store. The OpenFGA mode therefore has no
  global `/services/access` endpoint; a request without a cluster is a 400.
- **Authentication stays in kcp.** Organizations can have their own identity
  provider. A token from one is not accepted on a path without a workspace,
  because the front proxy only tries its global authenticators there.
- **Any workspace of the organization is accepted** (`root:orgs:foo:team-a`,
  or a cluster id) and gives the organization-wide answer. A workspace outside
  `root:orgs` gives an empty result.
- **Groups and service accounts resolve to nothing.** The OpenFGA model only
  knows users.

### Mapping accounts to clusters

An account object in OpenFGA is `<parent cluster id>/<account name>`. The
review must return the account's own cluster. Both lookups are plain reads
against kcp, remembered for a minute; nothing is watched. Two more reads
describe the account: the path of the parent cluster (its `kcp.io/path`),
which with the account name gives `account.path`, and the `Account` object
`<account name>` in `<parent cluster id>` for display name, type and
readiness (an account that is not ready is read again on every review). An
account that cannot be read is still listed, by name and path.

- organization name → store id: `status.storeId` of the `Store` named like the
  organization in `root:orgs`;
- account object → own cluster id: `spec.cluster` of the `Workspace`
  `<account name>` in `<parent cluster id>`.

## What exists

- **Code** in `operators/account-operator/virtual-workspace`: it answers for
  the tenancy model (accounts) that account-operator owns. A module of its
  own, `go.platform-mesh.io/account-operator/virtual-workspace`, because it
  builds against the kcp fork of Kubernetes; image
  `access-virtual-workspaces`, API group `access.platform-mesh.io`.
  - `accessprovider.AccessProvider` is the seam a review is answered through,
    for an `Identity` (name, groups, extra); `pkg/openfgaprovider` implements
    it, `GraphAdapter` wraps the RBAC graph, which a `GraphPopulator` fills.
    The names follow ADR 011.
  - `accessprovider.Union` merges several providers: a cluster any of them
    lists is listed once, and if one fails or is not ready the review fails,
    never a partial list. With OpenFGA among them a review still has to
    name a workspace.
  - Flags: `--provider=rbac|openfga|rbac,openfga`, `--openfga-addr`, `--openfga-relation`
    (`get`), `--openfga-object-type` (`core_platform-mesh_io_account`),
    `--openfga-orgs-workspace` (`root:orgs`).
  - Errors: no cluster → 400; outside `root:orgs` or no store → empty;
    account without a workspace → dropped and logged; OpenFGA or kcp error →
    503, never a partial list.
- **Chart** `charts/access-vw` (was `kcp-access-vw`): `provider: openfga` by
  default, the four OpenFGA settings under `openfga.*`, the external address
  from the operator-supplied `exposure`. In `openfga` mode there is no init
  container and no bootstrap kubeconfig.
- **Front-proxy**: `/services/access/clusters/{cluster}` and
  `/services/access` are in the default `additionalPathMappings` of the
  `infra` chart. The backend is named by its full cluster-local name, the only
  one in the serving certificate kcp-operator issues.
- **GraphQL gateway**: the chart registers the virtual workspace with the
  gateway as the `ClusterAccess` `access` (`clusterAccess.enabled`, on by
  default); platform-mesh-operator creates its kubeconfig Secret
  `access-vw-clusteraccess-kubeconfig`. This is how a browser asks a review:
  on the portal's origin, at the gateway endpoint `single-access` (the
  gateway prefixes the name with its provider), with the workspace in the
  `clusterTarget` extension and the mutation `createSelfClusterAccessReview`.
  The gateway validates the token against kcp for that workspace and
  forwards it. The API types carry OpenAPI model names so that the gateway
  finds the kind in the OpenAPI document.
- **Portal**: the Accounts list (`contentconfiguration-main-accounts.yaml`)
  sets `listView.accessReview`. A portal-ui-lib that knows the option lists
  the accounts from the review and falls back to listing the `Account`
  resources when the review cannot be answered; an older one ignores it.
- **Profiles**: `access-vw` is a service of the kind and production profiles,
  and referenced by the Platform Mesh OCM component.
- **Tilt**: component `access-vw` in the `api` group, built from the service
  and rolled out on every change. It answers from OpenFGA when OpenFGA is
  part of the run, else from RBAC; `--access-vw-provider` decides explicitly.
- **Tooling**: `hack/access-vw.sh --cluster <workspace>` with `--as` or
  `--token`.

Seen in Tilt with Dex tokens through the front proxy, in an organization
`bob` with the accounts `test` and `team-b`: its owner gets the three
workspaces, a member of `test` only that one, a user without a relation
nothing, and a review without a cluster is a 400.

## Open points

- **Identity of the server.** It reads Stores in `root:orgs` and Workspaces
  in every workspace of an organization, and is a kcp administrator
  (`system:kcp:admin`, `openfga.serverGroups`) for that. A narrower way to
  read across all organizations is needed.
- **Per-organization identity providers.** The Tilt environment with Dex has
  one global issuer, so that a token the plain path rejects is accepted on the
  cluster path has not been shown. Needs a run with Keycloak realms.
- **MCP virtual workspace** (`kcp-mcp-vw`, an image from kcp-dev). It calls
  the access virtual workspace on the plain path and with the upstream API
  group `access.contrib.kcp.io`. Neither works any more against this service
  in `openfga` mode; it needs a build that addresses a review to a workspace
  and uses `access.platform-mesh.io`.
- **Tests.** Unit tests only; no e2e suite (`e2e: false` in
  `ocm/components.yaml`), and the profiles with the new service have not been
  through `task tilt:ci`, the e2e or the upgrade suite. The release build of
  the chart and image components has not been run either.
- **Switching the provider of a running installation** changes the groups of
  the server identity. kcp-operator re-issued the certificate but wrote the
  kubeconfig Secret before that, so the pod kept the old identity until the
  Kubeconfig object was reconciled again.
- **Upstream.** The seam, the scoped path and the OpenFGA provider also exist
  as local changes in a checkout of the upstream repository; whether to
  contribute them back is undecided.

[kcp-dev/contrib-access-virtual-workspace]: https://github.com/kcp-dev/contrib-access-virtual-workspace
[kcp-vw-auth]: https://docs.kcp.io/kcp/main/concepts/authentication/workspace/#virtual-workspaces
