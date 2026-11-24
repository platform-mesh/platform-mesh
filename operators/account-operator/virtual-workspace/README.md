# access-virtual-workspaces

A [kcp](https://github.com/kcp-dev/kcp) virtual workspace that answers one
question: which workspaces can the caller access? The caller creates a
`SelfClusterAccessReview` (`access.platform-mesh.io/v1alpha1`) and gets it back
with `status.clusters` filled in, one entry per logical cluster with the URL
it is reached at. With the `openfga` provider each entry also has an `account`
block (`name`, `path`, `displayName`, `type`, `ready`), enough to list the accounts
without reading them. It is modelled on `SelfSubjectAccessReview`: nothing is
stored, and a caller only ever asks about itself.

It lives next to the account-operator because it answers for the tenancy
model that operator owns: with OpenFGA the workspaces in an answer are the
accounts of an organization. It is a module, image and chart of its own.

It is served behind the kcp front-proxy at `/services/access` and deployed by
the [access-vw](../../../charts/access-vw) chart, which is part of the Platform
Mesh profiles.

The code was imported from
[kcp-dev/contrib-access-virtual-workspace](https://github.com/kcp-dev/contrib-access-virtual-workspace)
and keeps its copyright headers.

## Providers

`--provider` selects where the answer comes from. `--provider=rbac,openfga`
runs both and merges their answers: a workspace either of them lists is
listed once. If one of them cannot answer, the review fails instead of
returning a partial list, and because `openfga` needs a workspace in the URL,
every review has to name one.

### `openfga` (what Platform Mesh runs)

The answer comes from OpenFGA; Kubernetes RBAC is not looked at. Nothing is
indexed: each review is one `ListObjects` call.

A review has to be addressed to a workspace of an organization, by path or by
logical cluster name:

```sh
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{}' \
  https://kcp.api.<base domain>/services/access/clusters/root:orgs:<organization>/apis/access.platform-mesh.io/v1alpha1/selfclusteraccessreviews
```

The result is the workspaces of the accounts of that organization on which
the caller has the relation `--openfga-relation` (default `get`). Access
inherits downward in the Platform Mesh model, so a member of the organization
sees all of its accounts and a member of one account sees that account and
what is below it. Any workspace inside the organization gives the same
answer.

- An organization is a workspace directly below `--openfga-orgs-workspace`
  (default `root:orgs`). Its OpenFGA store id is read from the `Store` object
  (`openfga.platform-mesh.io`) named like the organization in that workspace.
- An account is the OpenFGA object `<type>:<parent cluster>/<name>`
  (`--openfga-object-type`, default `core_platform-mesh_io_account`); its
  workspace is the `Workspace` `<name>` in `<parent cluster>`.
- The caller is `user:<username>`, colons replaced by dots. Groups are not
  part of the model.
- A review without a workspace is a 400. A workspace outside an organization
  gives an empty result. If OpenFGA or kcp cannot be asked, the review fails
  with 503; it never returns part of the answer.

The identity of `--kubeconfig` needs `get` on `stores` in the organizations
workspace and on `logicalclusters` and `workspaces` in every workspace of an
organization. The connection to OpenFGA (`--openfga-addr`) is not encrypted.

### `rbac`

The answer comes from an in-memory graph built from the `ClusterRoleBindings`
and `RoleBindings` of every workspace that binds the `access.platform-mesh.io`
APIExport ([config/examples](config/examples)). `cmd/init` installs that
export; the chart runs it as an init container. The workspace a review is
addressed to does not narrow the answer.

## Authentication

Behind the front-proxy the caller is authenticated there and arrives in the
request headers. The path mapping `/services/access/clusters/{cluster}` makes
the front-proxy use the authenticators of the workspace a review is addressed
to, in addition to the global ones; that is what accepts tokens of an
organization's own identity provider. On the plain `/services/access` path
only the global authenticators apply.

## Layout

| Path | Description |
|------|-------------|
| `cmd/access-vw` | The server. |
| `cmd/init` | Bootstrap of the `rbac` provider: workspace, APIExport, RBAC. |
| `cmd/scar-to-kubeconfig` | Client: turns a review into a kubeconfig with one context per workspace. |
| `sdk/apis/access/v1alpha1` | The `SelfClusterAccessReview` types. |
| `pkg/server` | Flags and wiring: serving, authentication, provider selection. |
| `pkg/virtual/scar` | The virtual workspace and the review storage. |
| `pkg/accessprovider` | The seams: `AccessProvider` answers a review, `GraphPopulator` feeds the graph; `Union` merges several providers. |
| `pkg/openfgaprovider` | The `openfga` provider. |
| `pkg/rbacprovider`, `pkg/graph` | The `rbac` provider and its graph. |
| `pkg/bootstrap`, `config` | What `cmd/init` installs. |

## Development

```sh
task build
task test
task lint
```

In the Tilt environment (`task tilt:up`) the component is `access-vw`: every
change here is compiled and rolled out. It runs the `openfga` provider when
OpenFGA is part of the run; `--access-vw-provider=rbac` selects the other one.
[`hack/access-vw.sh`](../../../hack/access-vw.sh) issues reviews against it:

```sh
hack/access-vw.sh --cluster root:orgs:<organization> --as <user e-mail>
```

The module builds against kcp's fork of Kubernetes (the `replace` block in
`go.mod`), like every virtual workspace of this repository; the pin has to
match the one of `github.com/kcp-dev/virtual-workspace-framework`.
