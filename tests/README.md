# tests

Tests that need a running control plane live here. Integration tests run
the components against a kcp in Docker; e2e tests run against a deployed
Platform Mesh. Unit tests stay next to the code they test and run with
`task test` or `task test-<component>`.

The code here is part of the root Go module, so a test imports helpers as
`go.platform-mesh.io/platform-mesh/tests/framework/...`.

## Two environments

| Environment | Tests | What you get |
|---|---|---|
| **kcp in Docker** | `integration/<area>` | A sharded kcp (front-proxy, cache server, two shards) started by [kcp-testcontainers](https://github.com/kcp-dev/kcp-testcontainers). Every test of a run shares it. The operators run inside the test process; tests keep out of each other's way by working in their own workspaces. |
| **kind** | `e2e/<area>` | One kind cluster with the whole stack deployed by Tilt. Tests treat it as a black box through `KUBECONFIG`, and `ADMIN_KUBECONFIG` for kcp. |

## The upgrade test

`upgrade/` is one test on an environment of its own: it creates a kind
cluster, installs the latest release into it, then upgrades it to this
checkout, built into a local registry. Both steps are the code behind
`pmctl quickstart`, the second (target for upgrade) one `--dev`, and each returns once
the installation is ready.

```sh
task test-upgrade                                  # from the latest release
task test-upgrade UPGRADE_FROM=v26.10.2            # from another one
task test-upgrade UPGRADE_KEEP_CLUSTER=true        # keep the cluster afterwards
```

It needs docker, go and task, container addresses that are routable from the
host (Linux, OrbStack), and port 8443 free. It builds every image of the
checkout, so it takes a while. A failed run keeps the cluster; its kubeconfig
is in `upgrade/.test/<cluster>/`. A plain `go test ./...` skips the test.

What has to survive the upgrade is described by scenarios, in
`upgrade/scenarios`. A scenario is something a user of the release has. It
lists three sets of steps:

- **Seed** creates it, on the release.
- **Validate** finds it and checks that it works. It runs on the release
  and again on the checkout, so it only reads.
- **After** goes on working with it on the checkout, once Validate held
  there: it changes, adds and deletes, which is where the new code has to
  cope with what the old code wrote.

A step is a function that is called again until it returns no error, like a
reconciler, so it does one pass and never waits itself; `framework/steps`
runs them.

| Scenario | What the release has | What After does with it |
|---|---|---|
| `tenant` | An organization with its owner, an invited user, an account, an account nested in it, and data in both. Validate also pins that they are the same objects (UIDs, logical clusters, authorization store, OIDC client), and that the owner gets in logged in: kcp, IAM service, gateway. | Changes the data, invites a user, adds and deletes accounts. |
| `roles` | A member role on an account, granted through the IAM service, and a guest without one. Both log in; kcp has to agree with what the IAM service reports. | Takes the role from the member, gives it to the guest, and back. |
| `isolation` | Two organizations. Neither owner gets into the other one, by role or by token. | |
| `offboarding` | An organization like any other. | Deletes its account and then the organization; the realm has to go too. |
| `provider` | A provider workspace exporting an API, an `APIExportPolicy`, and an account that bound the API and created an object. The owner lists it through kcp and the gateway; a guest does not. | Creates and deletes another object. |
| `platform` | `extraWorkspaces` and a scoped `extraProviderConnections` entry in the `PlatformMesh` object. The kubeconfig of the connection and the admin kubeconfig, as copied on the release, have to keep working. | Uses the kubeconfig of the connection again. |
| `extensions` | A `ContentConfiguration` in an account, processed. | Changes it; the result has to follow. |

```
upgrade/
  upgrade_test.go      TestUpgrade and the kind cluster it runs on
  scenarios/
    scenarios.go       the types, All() (the scenarios that run), Seed, Validate, After
    <scenario>.go      one file per scenario: its names and its steps, in order
    organization.go, invites.go, account.go, iam.go, gateway.go, identity.go
                       the steps, shared by the scenarios
    access.go          how steps reach the installation: as the admin, as a
                       user the admin impersonates, as a user who logged in
    state.go           what Seed remembers for Validate, in a Secret of the cluster
    manifests/         the objects that are easier to read as YAML
```

To add a scenario, write a file with a function returning it and list it in
`All()`. Three things to keep in mind:

- A scenario does not rely on another one: it brings its own organization.
  One that fails does not stop the others.
- Seed runs on the release with the Go types of this checkout. Create
  objects, and patch them; never update an object of Platform Mesh through
  its Go type, which drops every field the release has and the checkout
  does not know.
- Leave After so that Seed and Validate hold again on a kept cluster, or
  say why not.

The checks that an installation works at all, before and after the upgrade,
are still a placeholder (`verify` in `upgrade_test.go`).

## Writing an integration test

Ask for the shared kcp, create a workspace to work in, get a client for it.
That is the whole setup:

```go
func TestSomething(t *testing.T) {
	instance, err := kcp.Default(t.Context())
	require.NoError(t, err)
	path, err := instance.CreateWorkspace(t.Context(), "root:something-", kcptc.WaitForReachable())
	require.NoError(t, err)
	cl, err := instance.Client(path.String(), client.Options{Scheme: scheme.New(t)})
	require.NoError(t, err)
	// ...
}
```

A few things worth knowing:

- A path segment ending in `-` is a name prefix; kcp generates the rest, so
  tests never collide on workspace names.
- Creating a workspace returns once it is ready on its shard. The
  front-proxy that routes requests learns about new workspaces a moment
  later, so ask for the reachability wait as above unless you are happy to
  retry the first request yourself.
- Workspace type, shard and the other kcp workspace options are passed
  through `kcptc.With(...)`, which takes the options from the kcp SDK's
  testing package.

The `framework/` directory holds what the suites share:

- `framework/kcp` hands out the kcp. The default instance is shared by all
  suites; a suite whose setup clashes with the others gets an instance of
  its own (today only the security suite).
- `framework/fixtures` applies manifests and builds the
  `platform-mesh-system` workspace tree the operators expect: the
  operator's API exports and schemas, the bindings to them, the workspace
  types, and optionally the `orgs` workspace.
- `framework/pm` hands the e2e tests the deployed Platform Mesh they run
  against: the runtime cluster from `KUBECONFIG` and kcp from
  `ADMIN_KUBECONFIG`, and the Keycloak admin client from
  `KEYCLOAK_BASE_URL`, all of which `task tilt:e2e` sets.
- `framework/tenant` creates what a tenant owns on a deployed Platform
  Mesh, without the portal: an organization, accounts in it, invites to it.
  Organizations and accounts are created as their creator, whom the kcp
  admin impersonates, because Platform Mesh makes whoever sends the request
  the owner.
- `framework/keycloak` manages the users of a realm (every organization has
  one, named like it) through Keycloak's admin API: look one up, give it a
  password, delete it. It also logs a user in, the way a browser does, for
  a test that needs the token of a user.
- `framework/steps` runs a test as a list of steps that work like
  reconcilers: each is called again until it returns no error. The upgrade
  scenarios are written with it.
- `framework/manager` builds the multicluster managers the operators run on.
- `framework/wait` and `framework/scheme` are the waits and the scheme
  every suite needs.

## Running the integration tests

Docker is all you need.

```sh
task test-integration                                   # everything under tests/integration
task test-integration PKG=./integration/framework/...   # one area
task test-integration P=4                               # more packages at once (default 2)
task test-integration ADDITIONAL_COMMAND_ARGS=-v        # extra go test flags
task test-integration KCP_VERSION=v0.34.0               # another kcp release
```

The task runs the kcp version the repository deploys (the one in
`third_party/kcp`) unless `KCP_VERSION` says otherwise. From an IDE, run
the package as usual; without `KCP_VERSION` set, the framework falls back
to kcp-testcontainers' default release.

### One kcp per run

The first test to ask for the kcp starts it, which takes about 20 seconds.
Every later test, in the same or in another test binary, finds the running
containers by name and joins them. Running packages in parallel therefore
means several test binaries on one kcp, not several kcps. The containers
belong to the `go test` session; testcontainers removes them a few seconds
after the last test binary exits. Set `TESTCONTAINERS_RYUK_DISABLED=true`
to keep them around.

### Why it works from the host

The operators run in the test process on the host and follow the URLs kcp
hands out: workspace URLs, and the virtual workspace URLs their API exports
advertise. The framework therefore publishes the front-proxy and both shards
on free host ports and makes kcp advertise those host addresses, so a URL
from kcp works in a test exactly as it does for a deployed operator. The
framework suite checks this. Ports are picked per run; nothing needs 6443.
Every certificate kcp serves is valid for the Docker host, so no TLS
overrides are needed anywhere.

### Sharing one kcp between suites

Suites stay apart by working in their own generated workspaces. The one
shared piece is the `platform-mesh-system` tree the fixtures install, and
installing it is idempotent: whatever already exists is kept. That is also
its limit. Two suites that need different definitions of the same object
cannot share a kcp, in any order. Today that is the security suite: it
splits the stores and authorization models out of the core export into
separate exports, as deployed, while the account and framework suites
install the core export whole. So the security suite runs on its own kcp.

### Where the fixtures come from

Suites keep no copies of exports, schemas, bindings or workspace types.

- Exports and schemas come from each operator's generated API output under
  `apis/static-assets/resources/<operator>`. The generator only replaces
  the resources in an existing export, so permission claims and identity
  written by hand survive `task generate`.
- Bindings are derived from the export they bind.
- Workspace types come from the deployed kcp manifests under
  `apis/static-assets/kcp`. References to exports and types a suite did not
  install are dropped, and a suite can add the default bindings it needs on
  top.

## Logs

The test binary that starts a kcp streams each container's output to
`tests/.test/<instance>/`, which is gitignored: `front-proxy.log`,
`root.log`, `shard-b.log` and `cache.log`. CI uploads them when the job
fails. Operator logs go to the test output; `TEST_QUIET=1` silences them.

Workspaces are not deleted after a test. The kcp and everything in it go
away with the run, so to look at a failure afterwards, keep the containers
with `TESTCONTAINERS_RYUK_DISABLED=true`.

## Cleaning up

```sh
docker ps --filter name=platform-mesh   # instances still running (reaper disabled, or still winding down)
docker rm -f $(docker ps -aq --filter name=-platform-mesh-)
rm -rf tests/.test                      # all logs
```

## Integration areas

| Package | What it covers |
|---|---|
| `integration/framework` | Smoke test of the framework itself: the kcp starts, fixtures land in workspaces, the platform-mesh-system tree installs, advertised virtual workspace URLs answer from the host. |
| `integration/account` | account-operator: Accounts reconciled into Workspaces and AccountInfos. |
| `integration/extensionmanager` | extension-manager-operator: the ContentConfiguration controller. |
| `integration/graphqlgateway` | kubernetes-graphql-gateway: the listener controllers and the gateway serving GraphQL from kcp workspaces. |
| `integration/krocomposition` | kro-composition-operator: compositions, schema changes, type conflicts, pruning, consumers. |
| `integration/resourcebroker` | resource-broker across broker, coordination, compute, consumer and provider workspaces: assignment, copy, conditions, deletion, migration. |
| `integration/security` | security-operator: authorization model generation and invite validation. |
| `integration/subroutines` | The subroutines lifecycle library against a real API server with a test CRD. |
