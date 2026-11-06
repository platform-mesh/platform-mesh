# tests

The `go.platform-mesh.io/tests` module holds the tests that need a control
plane: integration tests against a kcp test server and e2e tests against a
deployed Platform Mesh. Unit tests stay next to the code they test
(`task test`, `task test-<component>`).

## Two environments

| Environment | Used by | What it is |
|---|---|---|
| **kcp** | `integration/<area>` (build tag `integration`) | A `sharded-test-server` (front-proxy, cache server, two shards) per test binary, or an existing kcp given by `--kcp-kubeconfig` / `KCP_KUBECONFIG`. Components run in-process; tests isolate by workspace. |
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
	_, path := env.Workspace(t, core.RootCluster.Path(), envtest.WithNamePrefix("something"))
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
task tests:integration:shared                           # one kcp for the whole run
```

The kcp binaries come from `task tools:kcp` (in `bin/`); the task exports
`TEST_KCP_ASSETS=<repo>/bin`, from which the framework resolves `kcp`,
`kcp-front-proxy`, `cache-server` and `sharded-test-server`. From an IDE, set
`TEST_KCP_ASSETS` and run the package with `-tags integration`.

### One server at a time: the lock and the fixed ports

Every integration package's `TestMain` is `kcp.Main`, which starts a private
`sharded-test-server` (front-proxy, cache server, two shards) for that test
binary. The server binds fixed ports on 127.0.0.1: 6443 (front-proxy), 6444 and
6445 (shards), 2379 to 2382 (the shards' embedded etcd), 8010 and 8011 (cache
server etcd), 8012 (cache server). Two private
servers cannot run on one host at the same time, so `kcp.Main` holds a
host-wide lock (`$TMPDIR/platform-mesh-kcp-test-server.lock`, flock) from
before the server starts until it is stopped. Other test binaries and
concurrent task invocations queue for it ("waiting for the kcp test server
lock") instead of failing.

So `P=` (`go test -p`) means: up to `P` packages are compiled and their test
binaries started in parallel, but the kcp servers, and therefore the tests,
run one package after the other. A larger `P` only overlaps compilation with
the running package; it does not run tests concurrently. Each package also
pays the server boot (about 20s). `tests:integration:shared` boots one server
for the whole run and lets every package attach to it (with `-p 1`).

Once it holds the lock, `kcp.Main` checks the ports. If they are busy, it
looks for a server left behind by a test binary that died without stopping
it (`go test -timeout`, a panic in a goroutine, `kill -9`): a
`sharded-test-server`, `kcp`, `kcp-front-proxy` or `cache-server` process
whose command line names a work dir under `tests/.test/` and whose parent is
gone. It logs such orphans ("killing orphaned kcp test server") and kills
their process group. Then it waits up to a minute for the ports; if they are
still busy, the package fails with the pid and command line of each port's
holder (usually a kcp started by hand or by Tilt on the same ports).

When a package ends, `kcp.Main` stops the server and kills whatever is left
of its process group, so no `kcp` process survives the test binary.

### Attaching to a running kcp

To run against a kcp you already have (e.g. one started by
`tests:integration:shared`, or the Tilt cluster's kcp), point
`KCP_KUBECONFIG` (or `-args --kcp-kubeconfig=<path>`) at its admin
kubeconfig; the framework uses the `base` context if present, else the
current one, and starts no server (and takes no lock):

```sh
KCP_KUBECONFIG=$PWD/tests/.test/shared-kcp/.kcp/admin.kubeconfig \
  TEST_KCP_ASSETS=$PWD/bin go test -tags integration -count=1 ./tests/integration/account/...
```

A package that attaches shares the kcp with whatever else uses it: tests
isolate by workspace (`env.Workspace` generates names), and
`fixtures.Install` reuses `:root:platform-mesh-system` and `:root:orgs`.
Install keeps objects that already exist, so packages that bring different
definitions of the same object cannot share a kcp in any order. Today that
is `integration/security`: it splits stores and authorizationmodels out of
`core.platform-mesh.io` into `fga.platform-mesh.io` and
`provider-fga.platform-mesh.io` (`fixtures.WithExport`, the deployed shape),
while `account` and `framework` install the export as apigen generates it,
serving the whole group. On a shared kcp where one of those ran first, the
security suite fails at once and says so. Run it on a private server
(`task test-integration PKG=./integration/security/...`).

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

## Logs and work dirs

Each private server runs in `tests/.test/<package import path with / as _>/`
(gitignored, wiped at the start of the package):

| File | What |
|---|---|
| `server.pid` | pid of the package's `sharded-test-server` (also its process group) |
| `.kcp-sharded-test-server/envtest.log` | the server's output (all shards, front-proxy, cache server) |
| `.kcp/admin.kubeconfig` | admin kubeconfig (contexts `base`, `root`, ...) |

When the server fails to start, the last 50 lines of `envtest.log` are
printed. `tests:integration:shared` writes to
`tests/.test/shared-kcp/sharded-test-server.log`. Controller logs go to the
test output; `TEST_QUIET=1` silences them.

Workspace fixtures are not deleted after a test (`PRESERVE=true` is the
default); a private server and its state go away when the package ends, so
to inspect a failure, run against a shared kcp and keep it running.

## Cleaning up

```sh
pgrep -fl sharded-test-server    # anything still running?
rm -rf tests/.test               # all work dirs and logs
```

A stray server under `tests/.test` whose test binary is gone is killed by the
next `kcp.Main` anyway; `pkill -f sharded-test-server` stops one by hand.

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
