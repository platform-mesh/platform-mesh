# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Install controller-gen (once)
task setup:controller-gen

# Generate CRD manifests + deepcopy
make generate

# Build binary
make build

# Run tests
make test

# Lint
make lint

# Coverage
make cover

# Docker image
make docker-build
```

Single-package test:
```bash
go test ./pkg/backup/... -run TestCapture
```

## Test tiers

There are three distinct test tiers, each with a different build tag and cluster requirement:

| Tier | Build tag | Command | What runs |
|---|---|---|---|
| Unit | _(none)_ | `make test` | Pure Go unit tests, no cluster |
| Simulated E2E | `e2e` | `make test-e2e-kind` | `pkg/e2e/` — uses in-process simulators for etcd-druid. No real pods, no minio. Fast (~15 min). |
| Real E2E | `e2e_real` | `make test-e2e-kind-real` | `pkg/e2e/real/` — minio deployed in-cluster, real etcd-druid + real etcdbr. Actual snapshots written and replayed. Slower (~40 min). |

The simulated tests (`pkg/e2e/`) stub out all external components and verify operator control-flow. The real tests (`pkg/e2e/real/`) prove the full round-trip works with real binaries.

To run a single real e2e test:
```bash
make test-e2e-kind-real
# or for a specific test: task test-e2e-kind-real -- -run TestRealEtcd_Restore_SingleShard
```

To run only the sharded real tests (requires a live platform-mesh deployment with Etcd CRs):
```bash
task test-e2e-kind-real -- -run TestRealEtcd_Sharded
```

## Architecture

This is a Kubernetes operator that orchestrates **etcd-druid** to back up and restore the etcd shards of a Platform Mesh deployment. It owns two cluster-scoped CRDs: `PlatformBackup` and `PlatformRestore`.

### API group
`backup.platform-mesh.io/v1alpha1` — defined in `api/v1alpha1/`.

- **PlatformBackup** — triggers a coordinated backup: discover kcp-shard Etcd CRs → fan-out parallel full snapshots via EtcdOpsTask → record per-shard snapshot keys in status artefacts.
- **PlatformRestore** — triggers a restore from a prior backup ID: validate topology → delete and recreate each Etcd CR with the restore annotation → wait for readiness.

Both types implement the `subroutines` conditions accessor interface (`GetConditions`/`SetConditions`, `GetObservedGeneration`/`SetObservedGeneration`, `GetNextReconcileTime`/`SetNextReconcileTime`).

### Package layout

```
pkg/
  backup/     EtcdCaptureSubroutine — snapshot fan-out, lease-key tracking
  restore/    EtcdRestoreSubroutine — non-blocking CR delete+recreate state machine
  topology/   ValidateSubroutine — strict shard-set comparison gate for restore
  controller/ PlatformBackup and PlatformRestore reconcilers
  config/     OperatorConfig (namespace, standalone flag)
pkg/e2e/      Simulated e2e tests (build tag: e2e)
pkg/e2e/real/ Real e2e tests with minio + etcd-druid (build tag: e2e_real)
```

### Controller pattern
Follows the **account-operator** conventions exactly:

- Controllers live in `pkg/controller/`.
- Each controller holds a `*lifecycle.Lifecycle` from `go.platform-mesh.io/subroutines/lifecycle` and delegates `Reconcile()` to it.
- Reconcilers are registered with the **multicluster-runtime** manager via `mcbuilder.ControllerManagedBy(mgr)` (not the standard controller-runtime builder).
- The manager is created with a **path-aware KCP provider** (`github.com/kcp-dev/multicluster-provider/path-aware`), enabling reconciliation across kcp logical clusters.

### Restore state machine

`EtcdRestoreSubroutine` is non-blocking. Each reconcile does one unit of work per shard and returns `Pending(5s)` if any shard is still in progress. The Etcd CR's own state drives all transitions:

1. CR absent → recreate with restore annotation → `Pending(5s)`
2. CR terminating → strip finalizers → `Pending(5s)`
3. Annotation set, not ready → `Pending(5s)`
4. Annotation set, ready → done
5. All shards done → `OK()` → `EtcdRestored=True`

### Topology validation

`topology.ValidateSubroutine` runs before `EtcdRestoreSubroutine`. With `TopologyValidation=Strict` it compares the live kcp-shard Etcd CRs against the shards recorded in the backup artefact. Any mismatch stops the chain with `StopWithRequeue(5s)` until the cluster topology matches.

### Entry point
`main.go` → `cmd.Execute()` → Cobra root (`cmd/root.go`) registers the scheme and adds the `operator` sub-command → `cmd/operator.go` builds the `mcmanager`, wires both controllers, and calls `mgr.Start()`.

### Code generation
`zz_generated.deepcopy.go` is produced by `controller-gen object:headerFile=hack/boilerplate.go.txt paths=./...`.
CRD YAMLs in `config/crd/` are produced by `controller-gen rbac:roleName=manager-role crd paths=./... output:crd:artifacts:config=config/crd`.
Both are committed and must be regenerated whenever API types change (`make generate`).

### Dependency versions
- `sigs.k8s.io/controller-runtime v0.23.3`
- `sigs.k8s.io/multicluster-runtime v0.23.3`
- `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go` — all `v0.35.4`
- `go.platform-mesh.io/subroutines v0.6.0`
- `github.com/kcp-dev/multicluster-provider v0.7.1`
