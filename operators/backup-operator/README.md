> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# Platform Mesh - backup-operator

## Description

The Platform Mesh `backup-operator` orchestrates [etcd-druid](https://github.com/gardener/etcd-druid) to back up and restore the etcd shards of a Platform Mesh deployment running on [kcp](https://github.com/kcp-dev/kcp). It owns two cluster-scoped Custom Resources from the `backup.platform-mesh.io/v1alpha1` API group:

- **`PlatformBackup`** — triggers a coordinated backup: discover kcp-shard Etcd CRs → fan-out parallel full snapshots via `EtcdOpsTask` → record per-shard snapshot keys in status artefacts.
- **`PlatformRestore`** — triggers a restore from a prior backup: validate cluster topology against the backup artefact → delete and recreate each Etcd CR with a restore annotation so etcdbr replays from the recorded snapshot.

## Features

- Declarative `PlatformBackup` resource with per-shard snapshot fan-out and idempotent retry
- Declarative `PlatformRestore` resource with strict topology validation gate
- Non-blocking restore state machine — no goroutine held between reconciles
- Multi-cluster reconciliation across kcp logical clusters via [multicluster-runtime](https://github.com/platform-mesh/multicluster-runtime)
- Reconciliation built on the [subroutines](../../subroutines) lifecycle engine

## How etcd backup and restore works

**Backup** — the operator enumerates every Etcd CR labelled
`platform-mesh.io/component=kcp-shard`, creates an `EtcdOpsTask` (on-demand full
snapshot) per shard in parallel, waits for each task to reach `Succeeded`, reads the
snapshot key from the full-snap coordination lease, and records per-shard keys in
`PlatformBackup.Status.Artefacts.Etcd.Shards`.

**Restore** — with `TopologyValidation=Strict` the operator first verifies that the live
kcp-shard Etcd CRs exactly match the shards recorded in the backup artefact. Once
topology is confirmed, it drives a non-blocking state machine per shard:
1. Delete the existing Etcd CR (with `GracePeriod=0`, finalizers stripped first)
2. Recreate it with the same spec and the restore annotation `backup.platform-mesh.io/restored-from-snapshot=<key>`
3. Wait for `status.ready=true` — etcdbr automatically restores from the latest snapshot at the configured prefix on pod startup

`EtcdRestored=True` is set only when every shard reaches the ready state.

## Package layout

```
cmd/                        Cobra CLI entry-point
pkg/
  backup/                   EtcdCaptureSubroutine + unit & integration tests
  restore/                  EtcdRestoreSubroutine (non-blocking) + unit & integration tests
  topology/                 ValidateSubroutine — strict shard-set comparison gate
  controller/               PlatformBackup/Restore reconcilers (wires subroutines)
  config/                   OperatorConfig (--namespace, --standalone flags)
  e2e/                      Simulated e2e tests (build tag: e2e)
  e2e/real/                 Real e2e tests with minio + etcd-druid (build tag: e2e_real)
config/
  crd/                      Generated CRD YAML (committed; regenerate with make generate)
  deploy/                   Deployment, ClusterRole, Role manifests for kind
  resources/                KCP APIExport and APIResourceSchema manifests
```

## Development

```bash
# Generate CRDs + deepcopy + RBAC
make generate

# Build binary
make build

# Format + lint
make lint
```

## Testing

There are three test types with increasing infrastructure requirements:

| Type | Build tag | Command | Needs cluster? | Typical runtime |
|---|---|---|---|---|
| **Unit** | _(none)_ | `make test` | No | < 5 s |
| **Integration** | `integration` | `make test-integration` | No (in-process API server via envtest) | ~30 s |
| **Simulated E2E** | `e2e` | `make test-e2e-kind` | Yes (kind cluster) | ~15 min |
| **Real E2E** | `e2e_real` | `make test-e2e-kind-real` | Yes (kind cluster + minio + CNPG) | ~60 min |

### Unit tests

Tests subroutine logic using a fake in-memory Kubernetes client. No cluster required.

```bash
make test
# or: go test -count=1 ./...
```

### Integration tests (envtest)

Spins up a real API server in-process via `controller-runtime/envtest` with etcd-druid CRDs loaded from the Go module cache.

```bash
make test-integration
```

### Simulated E2E tests (kind cluster)

The simulated e2e suite requires a **Platform Mesh kind cluster** running locally with etcd-druid deployed. Tests use in-process simulators — a task simulator goroutine completes `EtcdOpsTask` CRs immediately, and a ready simulator sets `status.ready=true` on Etcd CRs. All test Etcd CRs carry `druid.gardener.cloud/suspend-etcd-spec-reconcile=true` so etcd-druid does not create real StatefulSets.

```bash
make test-e2e-kind
```

To run specific tests:
```bash
task test-e2e-kind -- -run TestEtcDruid_Restore_TopologyMismatch_SelfHealing
```

### Real E2E tests (kind cluster + minio)

The real e2e suite deploys minio into the cluster and uses real etcd-druid and etcdbr. Actual snapshots are written to minio and replayed during restore.

```bash
make test-e2e-kind-real
```

To run specific real e2e tests by component:
```bash
make test-e2e-kind-real-etcd    # etcd-druid tests only
make test-e2e-kind-real-cnpg    # CNPG tests only
make test-e2e-kind-real-velero  # Velero tests only (Velero deployed by operator)
```

To run a single test:
```bash
task test-e2e-kind-real -- -run TestRealEtcd_Backup_ContentIntegrity
```

#### Prerequisites for e2e tests

| Prerequisite | How to verify |
|---|---|
| Platform Mesh kind cluster named `platform-mesh` running | `kind get clusters` |
| etcd-druid deployed | `kubectl get deploy -A \| grep etcd-druid` |
| `kubectl` context pointing at the kind cluster | `kubectl config current-context` |
| Docker or Podman available | `docker info` / `podman info` |

#### Environment variables

| Variable | Default | Description |
|---|---|---|
| `KUBECONFIG` | `~/.kube/config` | Kubeconfig for the kind cluster |
| `E2E_NAMESPACE` | `platform-mesh-backup-operator` | Namespace for operator and test objects |
| `KIND_CLUSTER` | `platform-mesh` | Kind cluster name used by `make deploy-kind` |
| `CONTAINER_RUNTIME` | `docker` | `docker` or `podman` for image build/load |

## Getting started

- For running and building the backup-operator, please refer to the [CONTRIBUTING.md](../../CONTRIBUTING.md) file in this repository.
- To deploy the backup-operator to kubernetes, please refer to the [helm-charts](https://github.com/platform-mesh/helm-charts) repository.

## Releasing

The release is performed automatically through a GitHub Actions Workflow.
All the released versions will be available through access to GitHub (as any other Golang Module).

## Requirements

The backup-operator requires an installation of Go. Checkout the [go.mod](go.mod) for the required Go version and dependencies.

## Contributing

Please refer to the [CONTRIBUTING.md](../../CONTRIBUTING.md) file in this repository for instructions on how to contribute to Platform Mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
