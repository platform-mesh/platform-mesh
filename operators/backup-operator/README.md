> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# Platform Mesh - backup-operator

## Description

The Platform Mesh `backup-operator` orchestrates [Velero](https://velero.io/), [CloudNativePG](https://cloudnative-pg.io/), and [etcd-druid](https://github.com/gardener/etcd-druid) to back up and restore a Platform Mesh deployment running on [kcp](https://github.com/kcp-dev/kcp). It owns two cluster-scoped Custom Resources from the `backup.platform-mesh.io/v1alpha1` API group:

- **`PlatformBackup`** — triggers a coordinated backup: topology capture, then parallel etcd snapshots, CloudNativePG base backups, and a Velero backup, writing a `topology.json` manifest to object storage.
- **`PlatformRestore`** — triggers a restore from a prior backup: fetch and validate the captured topology, run sequential component restores, then repair (e.g. orphan tuple sweep).

## Features
- Declarative `PlatformBackup` resource coordinating etcd, CloudNativePG, and Velero backups
- Declarative `PlatformRestore` resource with configurable topology validation
- Multi-cluster reconciliation across kcp logical clusters via [multicluster-runtime](https://github.com/platform-mesh/multicluster-runtime)
- Reconciliation built on the [subroutines](../../subroutines) lifecycle engine

## How etcd backup and restore works

**Backup** — the operator enumerates every Etcd CR labelled
`platform-mesh.io/component=kcp-shard`, creates an `EtcdOpsTask` (on-demand full
snapshot) per shard in parallel, waits for each task to reach `Succeeded`, reads the
snapshot key from the full-snap coordination lease, and records per-shard keys in
`PlatformBackup.Status.Artefacts.Etcd.Shards`.

**Restore** — the operator looks up the source `PlatformBackup`, deletes each Etcd CR,
waits for deletion, recreates it with the original spec (same `Spec.Backup.Store.Prefix`),
and waits for `status.ready=true`. The etcdbr sidecar inside each Etcd pod automatically
restores from the latest snapshot at that prefix on startup. All shards are processed
concurrently; `EtcdRestored=True` is set only when every shard succeeds.

## Package layout

```
cmd/                        Cobra CLI entry-point
pkg/
  backup/                   EtcdCaptureSubroutine + unit & integration tests
  restore/                  EtcdRestoreSubroutine + unit & integration tests
  controller/               PlatformBackup/Restore reconcilers (wires subroutines)
  config/                   OperatorConfig (--namespace, --standalone flags)
  topology/                 topology.json schema and validation
  e2e/                      End-to-end tests against a live kind cluster
config/
  crd/                      Generated CRD YAML (committed; regenerate with task generate)
  rbac/                     Generated RBAC role YAML
  deploy/                   Deployment, ClusterRole, Role manifests for kind
  resources/                KCP APIExport and APIResourceSchema manifests
```

## Development

```bash
# Install controller-gen (once)
task tools:setup

# Generate CRDs + deepcopy + RBAC
task generate

# Build binary
task build

# Format + lint
task lint
```

## Testing

There are three test types with increasing infrastructure requirements:

| Type | Build tag | Makefile target | Needs cluster? | Typical runtime |
|---|---|---|---|---|
| **Unit** | _(none)_ | `make test` | No | < 5 s |
| **Integration** | `integration` | `make test-integration` | No (in-process API server) | ~30 s |
| **E2E** | `e2e` | `make test-e2e-kind` | Yes (kind cluster) | ~15 min |

### Unit tests

Tests the subroutine logic — `EtcdCaptureSubroutine` and `EtcdRestoreSubroutine` —
using a fake in-memory Kubernetes client. No API server, no cluster, no external
tooling required.

```bash
make test
# or: go test -count=1 -tags '!integration' ./...
```

### Integration tests (envtest)

Spins up a real API server **in-process** via `controller-runtime/envtest` with
etcd-druid CRDs loaded from the Go module cache. Tests the full subroutine lifecycle
including real object creation, status updates, and lease reads. No cluster needed —
only the Go module cache must be populated (`go mod download` ensures this).

```bash
make test-integration
# or: task test-e2e  (runs go test -count=1 -tags integration ./...)
```

### End-to-end tests (kind cluster)

The e2e suite requires a **Platform Mesh kind cluster** already running on the local
machine. The cluster is named `platform-mesh` by convention and must have
`etcd-druid` deployed — both are part of the standard Platform Mesh development
environment. If you do not yet have this cluster, follow the
[platform-mesh local setup guide](../../CONTRIBUTING.md) before continuing.

The tests themselves do **not** require real etcd pods, a running kcp workspace, or a
real object store. They use a _task simulator_ — a goroutine that completes
`EtcdOpsTask` CRs to `Succeeded` at 50 ms intervals, racing ahead of etcd-druid's
own controller. All test Etcd CRs carry
`druid.gardener.cloud/suspend-etcd-spec-reconcile=true` so etcd-druid does not create
StatefulSets for them. The tests exercise the full operator control-flow (task
creation, lease reading, status recording, CR recreation, readiness waiting) without
depending on real etcd pods or a real object store.

#### Prerequisites

| Prerequisite | How to verify |
|---|---|
| Platform Mesh kind cluster named `platform-mesh` running locally | `kind get clusters` |
| etcd-druid deployed (any namespace) | `kubectl get deploy -A \| grep etcd-druid` |
| `kubectl` context pointing at the kind cluster | `kubectl config current-context` |
| Docker or Podman available | `docker info` / `podman info` |

> **Note** — the cluster name and namespace can be overridden via the `KIND_CLUSTER`
> and `E2E_NAMESPACE` environment variables if your local setup uses different names.

#### Run (build + deploy + test)

```bash
task test-e2e-kind
```

`deploy:kind` is content-addressed: it hashes all Go source files and the Dockerfile. If
the running deployment already carries that hash as its image tag, the build and load
steps are skipped and only the tests execute.

#### Run tests only (operator already deployed and up-to-date)

```bash
KUBECONFIG=$HOME/.kube/config \
E2E_NAMESPACE=platform-mesh-system \
go test -tags e2e -count=1 -timeout 15m -v ./pkg/e2e/...
```

#### Redeploy after source changes

```bash
task deploy:kind
```

#### Environment variables

| Variable | Default | Description |
|---|---|---|
| `KUBECONFIG` | `~/.kube/config` | Kubeconfig for the kind cluster |
| `E2E_NAMESPACE` | `platform-mesh-system` | Namespace where operator and test objects live |
| `KIND_CLUSTER` | `platform-mesh` | Kind cluster name used by `deploy:kind` |
| `CONTAINER_RUNTIME` | `docker` | `docker` or `podman` for image build/load |

#### Test tag summary

| Build tag | Task | What runs |
|---|---|---|
| _(none)_ | `task test` | Unit tests only (no cluster, no envtest) |
| `integration` | `task test-e2e` | Envtest suites — etcd-druid CRDs, no cluster needed |
| `e2e` | `task test-e2e-kind` | Live kind cluster tests |

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
