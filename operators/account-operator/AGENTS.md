## Repository Description
- `account-operator` manages `Account` and `AccountInfo` resources for Platform Mesh.
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. Prefer the narrowest fix that solves the real problem.
- Verify behavior before finishing. Run the smallest relevant tests first, then broader checks if needed.
- Prefer existing repo workflows over ad-hoc commands.
- Keep human-facing process details in `CONTRIBUTING.md`; keep this file focused on agent execution.

## Project Structure
- `api/v1alpha1`: API types, webhooks, generated deepcopy code.
- `internal/controller`: reconcilers and controller tests.
- `internal/config`: runtime configuration parsing and tests.
- `pkg/subroutines`: reusable reconciliation subroutines and mocks.
- `config/crd`: generated CRDs.
- `config/resources` and `test/setup`: generated API resources used by runtime and tests.
- `cmd` and `main.go`: CLI and process entrypoints.
- `hack`: tooling helpers and boilerplate for generation.

## Architecture
This is not a standard controller-runtime operator. Read this before touching reconcilers or subroutines.

### Runtime model
- The manager is an `mcmanager.Manager` from `sigs.k8s.io/multicluster-runtime`, backed by an `apiexport.New(...)` provider from `kcp-dev/multicluster-provider` bound to `operatorCfg.Kcp.ApiExportEndpointSliceName` (default `core.platform-mesh.io`).
- Reconcilers use `mcreconcile.Request` (not `reconcile.Request`) and `mcbuilder.ControllerManagedBy(mgr)`.
- Cluster clients come in three flavors — pick the right one deliberately:
  - **Per-request workspace client** via the `ClusterName` on `mcreconcile.Request` — use this to read/write resources inside the account's workspace. Do not use the manager's client for workspace resources.
  - **`mgr.GetLocalManager()`** — the provider/local cluster; used for webhooks and as the base config for the orgs client.
  - **`orgsClient`** (built in `cmd/operator.go:buildOrgsClient` by rewriting the host to `/clusters/root:orgs`) — passed to subroutines that must operate in the orgs workspace (`WorkspaceType`, `Workspace`).

### Reconciliation: subroutine chain
Both controllers delegate `Reconcile` to `lifecycle.Lifecycle.Reconcile` from `github.com/platform-mesh/subroutines`, which runs an ordered list of `subroutines.Subroutine` implementations.

- `AccountReconciler` runs: `workspacetype` → `workspace` → `manageaccountinfo` → `workspaceready`, each gated by a flag in `config.SubroutinesConfig`.
- `AccountInfoReconciler` runs only `finalizeaccountinfo`, gated by `--controllers-account-info-enabled`.
- Status conditions on `Account` are managed by `conditions.NewManager()` wired into the lifecycle. Do not set conditions manually inside subroutines.
- Rate limiting uses `ratelimiter.NewStaticThenExponentialRateLimiter` from `platform-mesh/golang-commons`, not controller-runtime's default.

### Domain model
- `Account` is cluster-scoped and lives in the parent workspace. `Spec.Type` ∈ `{org, account}`; additional types can be allow-listed via `--webhooks-additional-account-types`.
- `Spec.Extensions` is a list of go-template'd arbitrary resources the operator instantiates in the account workspace; each has an optional `ReadyConditionType` to gate readiness.
- `AccountInfo` is cluster-scoped and lives **inside the account's own workspace**. It stores derived metadata: account/parent/org `AccountLocation` (`path`/`url`/`generatedClusterId`/`originClusterId`/`type`), FGA store id, cluster CA, and OIDC clients.
- The validating webhook denies names on `--webhooks-deny-list`, validates `Type` against the allow-list, and enforces immutability of fields that must not change after creation. There is no mutating webhook.

### Configuration
- `internal/config/config.go` defines `OperatorConfig` and exposes every subroutine/controller/webhook/kcp setting via cobra flags in `cmd/root.go`. Defaults enable all subroutines and controllers.
- **Adding a subroutine**: implement the `subroutines.Subroutine` interface in a new package under `pkg/subroutines/`, add a `SubroutineConfig` entry in `internal/config/config.go` with a matching flag in `AddFlags`, then wire it into the reconciler's subroutine list in `internal/controller/`.
- `defaultCfg` is `platformmeshconfig.CommonServiceConfig` from `platform-mesh/golang-commons` — controls metrics, health probes, leader election, tracing, log level, `MaxConcurrentReconciles`, and `DebugLabelValue` (used in a predicate filter applied to both controllers).
- Leader election uses a separate `rest.InClusterConfig()` (not the APIExport config) so the lock is held against the local cluster.

### Tests
- Controller tests under `internal/controller/` use kcp envtest (hence `task setup:kcp`). The `test/setup/01-platform-mesh-system/` directory contains generated API resources consumed by test bootstrap — regenerate via `task generate`, do not hand-edit.
- Subroutine unit tests mock the multicluster manager and clients from `pkg/subroutines/mocks/` (generated by `mockery`).

## Commands
- `task fmt` — format Go code.
- `task lint` — run formatting plus golangci-lint.
- `task envtest` — run Go tests without bootstrapping extra tools.
- `task test` — run the standard local test path with required tooling (kcp + gomplate).
- `task cover` — envtest with coverage; thresholds in `.testcoverage.yml` (80% total, 70% controllers package, 60% per controller file).
- `go test ./pkg/subroutines/<pkg>/ -run TestName -v` — single-test fallback for targeted verification.
- `task manifests` — regenerate CRDs.
- `task generate` — regenerate deepcopy code and API resource output after API changes; runs `apigen` into both `config/resources` (runtime) and `test/setup/01-platform-mesh-system` (test bootstrap).
- `docker build .` — build the container image.
- `task docker:kind` — build image with the tag currently deployed, load into kind (cluster `platform-mesh` by default, override with `KIND_CLUSTER=…`), restart the `account-operator` deployment in `platform-mesh-system`.
- Mocks are regenerated by `mockery` per `.mockery.yaml` into `pkg/subroutines/mocks/` (only controller-runtime `Client`/`Cluster` and multicluster-runtime `Manager` interfaces).
- Go toolchain: `go.mod` pins Go 1.26; CI enforces it.

## Code Conventions
- Follow existing Go patterns in the touched package before introducing new abstractions.
- Keep controller logic in `internal/controller`; put reusable reconciliation helpers in `pkg/subroutines`.
- Add or update `_test.go` files next to changed production code.
- When editing API types under `api/v1alpha1`, regenerate derived files instead of hand-editing generated output.
- Never edit `api/v1alpha1/zz_generated.deepcopy.go` manually.
- Keep logging structured and avoid logging secrets or full credentials.

## Generated Artifacts
- If CRD schemas or API types change, run `task generate`.
- Review generated changes in `config/crd`, `config/resources`, and `test/setup`.
- Do not mix unrelated manual edits into generated files.

## Do Not
- Edit `api/v1alpha1/zz_generated.deepcopy.go` directly.
- Edit generated files in `config/crd` or `config/resources` without running `task generate`.
- Skip regeneration after changing API types or CRD schema.

## Hard Boundaries
- Do not invent new build or test workflows when a `task` target already exists.
- Do not move code across packages unless the change actually requires it.
- Ask before making changes that affect release flow, CI wiring, container publishing, or Helm chart integration outside this repository.

## Human-Facing Guidance
- Use `README.md` for local setup and high-level context.
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.
