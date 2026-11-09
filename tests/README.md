# tests

The tests that need a control plane: integration tests against a kcp in
Docker and e2e tests against a deployed Platform Mesh. The Go code here is
part of the root module (`go.platform-mesh.io/platform-mesh`); unit tests
stay next to the code they test (`task test`, `task test-<component>`).

## Two environments

| Environment | Used by | What it is |
|---|---|---|
| **kcp** | `integration/<area>` (build tag `integration`) | A sharded kcp (front-proxy, cache server, two shards) in Docker via [kcp-testcontainers](https://github.com/kcp-dev/kcp-testcontainers), shared by every test binary of the run, or an existing kcp given by `--kcp-kubeconfig` / `KCP_KUBECONFIG`. Components run in-process; tests isolate by workspace. |
| **kind** | `e2e/<area>` (build tag `e2e`) | One kind cluster with the stack deployed by Tilt. Tests are black-box against `KUBECONFIG` (and `ADMIN_KUBECONFIG` for kcp). |

Everything else is a helper in `framework/`:

- `framework/kcp`: `kcp.Main` (the `TestMain` of every integration package), `kcp.Server(t)`, clients, workspaces.
- `framework/fixtures`: apply manifests (`Decode`, `Apply`, `ApplyFS`) and `Install` the platform-mesh-system tree (operator resources, bindings, WorkspaceTypes, `:root:orgs`).
- `framework/manager`: multicluster managers over kcp APIExport providers.
- `framework/wait`, `framework/scheme`: the waits and scheme every suite needs.

A new integration package:

```go
//go:build integration

func TestMain(m *testing.M) { os.Exit(kcp.Main(m)) }

func TestSomething(t *testing.T) {
	env := kcp.Server(t)
	_, path := env.Workspace(t, core.RootCluster.Path(), kcptesting.WithNamePrefix("something"))
	cl := env.Client(t, path, scheme.New(t))
	// ...
}
```

## Running the integration tests

```sh
task test-integration                                   # all of tests/integration
task test-integration PKG=./integration/framework/...   # one area
task test-integration P=4                               # more packages at once (default 2)
task test-integration ADDITIONAL_COMMAND_ARGS=-v        # extra go test flags
task test-integration KCP_IMAGE=ghcr.io/kcp-dev/kcp:main # another kcp
```

Docker is all that is needed. The task runs the repository's kcp version
(`third_party/kcp`, the one Tilt deploys) unless `KCP_IMAGE` says otherwise.
From an IDE, run the package with `-tags integration`; without `KCP_IMAGE`
the framework uses kcp-testcontainers' default image.

### One kcp per run, in Docker

Every integration package's `TestMain` is `kcp.Main`. The first test binary
of a `go test` run starts the kcp named `platform-mesh` (front-proxy, cache
server, shards `root` and `shard-b`, about 20s); the others find it by name
and join. So `P=` (`go test -p`) really runs packages concurrently, all on
one kcp. The containers belong to the go test session and testcontainers'
reaper removes them a few seconds after the last test binary has exited
(`TESTCONTAINERS_RYUK_DISABLED=true` keeps them).

Containers talk to each other over a Docker network, but the operators run
in-process on the host and follow the URLs kcp advertises: workspace URLs
and the APIExport virtual workspace URLs in APIExportEndpointSlices. The
framework therefore publishes the front-proxy and both shards on free host
ports and makes the shards advertise the host side
(`--shard-external-url`, `--shard-virtual-workspace-url`), so a URL from
kcp works from the test as it would for a deployed operator
(`integration/framework` checks this). Ports are picked per run; nothing
needs 6443.

A package whose fixtures cannot share a kcp with the others (see below)
names its own instance: `kcp.Main(m, kcp.WithInstance("security"))`.
`kcp.WithFeatureGates(...)` enables kcp feature gates on an instance; every
package naming the instance must ask for the same ones.

### Attaching to a running kcp

To run against a kcp you already have (e.g. the Tilt cluster's kcp), point
`KCP_KUBECONFIG` (or `-args --kcp-kubeconfig=<path>`) at its admin
kubeconfig; the framework uses the `base` context if present, else the
current one, and starts nothing:

```sh
. contrib/tilt/env.sh   # exports ADMIN_KUBECONFIG for the Tilt cluster's kcp
KCP_KUBECONFIG=$ADMIN_KUBECONFIG go test -tags integration -count=1 ./tests/integration/account/...
```

### Sharing a kcp

Packages sharing a kcp isolate by workspace (`env.Workspace` generates
names), and `fixtures.Install` reuses `:root:platform-mesh-system` and
`:root:orgs`. Install keeps objects that already exist, so packages that
bring different definitions of the same object cannot share a kcp in any
order. Today that is `integration/security`: it splits stores and
authorizationmodels out of `core.platform-mesh.io` into
`fga.platform-mesh.io` and `provider-fga.platform-mesh.io`
(`fixtures.WithExport`, the deployed shape), while `account` and `framework`
install the export as apigen generates it, serving the whole group. On a
shared kcp where one of those ran first, the security suite fails at once
and says so, which is why it runs on its own instance.

Exports and schemas always come from the operator's apigen output in
`apis/static-assets/resources/<operator>` (`fixtures.WithResources` on its
embedded `Resources`; apigen reads the existing APIExport back and replaces
only its resources, so hand-written permission claims and identity survive
`task generate`); bindings are derived from the export they bind
(`fixtures.BindingFor`, `fixtures.WithBindings(exportName)`); WorkspaceTypes
come from the deployed kcp manifests in `apis/static-assets/kcp`, with
bindings to exports and extends of types that are not installed dropped,
plus `fixtures.WithDefaultBindings` for what a suite needs on top. Suites
keep copies of none of these.

## Logs

The test binary that starts an instance streams the containers' output to
`tests/.test/<instance>/` (gitignored): `front-proxy.log`, `root.log`,
`shard-b.log` and `cache.log`. CI uploads them when the job fails.
Controller logs go to the test output; `TEST_QUIET=1` silences them.

Workspace fixtures are not deleted after a test (`PRESERVE=true` is the
default); the kcp and its state go away with the run, so to inspect a
failure, keep the containers (`TESTCONTAINERS_RYUK_DISABLED=true`) or run
against a kcp of your own via `KCP_KUBECONFIG`.

## Cleaning up

```sh
docker ps --filter name=platform-mesh   # instances still running (reaper disabled, or still winding down)
docker rm -f $(docker ps -aq --filter name=-platform-mesh-)
rm -rf tests/.test                      # all logs
```

## Integration areas

| Package | What it covers |
|---|---|
| `integration/framework` | Smoke test of `tests/framework`: the kcp environment starts or attaches, fixtures land in workspaces, `fixtures.Install` builds the platform-mesh-system tree. |
| `integration/account` | account-operator in-process: Accounts reconciled into Workspaces and AccountInfos. |
| `integration/backup` | backup-operator topology projector against a kcp workspace. |
| `integration/extensionmanager` | extension-manager-operator ContentConfiguration controller. |
| `integration/graphqlgateway` | kubernetes-graphql-gateway listener controllers (ClusterAccess, resource) and the gateway serving GraphQL from kcp workspaces. |
| `integration/kcpmigration` | kcp-migration-operator wired through its `pkg/app`: KCPMigration phases and object sync between workspaces. |
| `integration/krocomposition` | kro-composition-operator engine: compositions, schema changes, type conflicts, pruning, consumers. |
| `integration/resourcebroker` | resource-broker across broker, coordination, compute, consumer and provider workspaces: assignment, copy, conditions, deletion, migration. |
| `integration/security` | security-operator: authorization model generation and invite validation. |
| `integration/subroutines` | subroutines lifecycle library against a real API server with a test CRD. |
