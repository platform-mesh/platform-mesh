# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Install controller-gen (once)
task setup:controller-gen

# Generate CRD manifests + deepcopy
task generate          # runs manifests then controller-gen object

# Build binary
task build             # output: bin/manager

# Run tests
task test              # requires kcp + gomplate (downloaded automatically)

# Lint
task lint

# Coverage
task cover

# Docker image
task docker-build
```

Single-package test:
```bash
go test ./internal/controller/... -run TestFoo
```

## Architecture

This is a Kubernetes operator that orchestrates **Velero**, **CloudNativePG**, and **etcd-druid** to back up and restore a Platform Mesh deployment. It owns two cluster-scoped CRDs: `PlatformBackup` and `PlatformRestore`.

### API group
`backup.platform-mesh.io/v1alpha1` — defined in `api/v1alpha1/`.

- **PlatformBackup** — triggers a coordinated backup: topology capture → parallel (etcd snapshots, CNPG base backups, Velero backup) → writes a `topology.json` manifest to S3.
- **PlatformRestore** — triggers a restore from a prior backup ID: fetch topology.json → validate topology → sequential component restores → repair (orphan tuple sweep).

Both types implement the `subroutines` conditions accessor interface (`GetConditions`/`SetConditions`, `GetObservedGeneration`/`SetObservedGeneration`, `GetNextReconcileTime`/`SetNextReconcileTime`).

### Controller pattern
Follows the **account-operator** conventions exactly:

- Controllers live in `internal/controller/`.
- Each controller holds a `*lifecycle.Lifecycle` from `go.platform-mesh.io/subroutines/lifecycle` and delegates `Reconcile()` to it.
- Reconcilers are registered with the **multicluster-runtime** manager via `mcbuilder.ControllerManagedBy(mgr)` (not the standard controller-runtime builder).
- The manager is created with a **path-aware KCP provider** (`github.com/kcp-dev/multicluster-provider/path-aware`), enabling reconciliation across kcp logical clusters.

### Entry point
`main.go` → `cmd.Execute()` → Cobra root (`cmd/root.go`) registers the scheme and adds the `operator` sub-command → `cmd/operator.go` builds the `mcmanager`, wires both controllers, and calls `mgr.Start()`.

### Code generation
`zz_generated.deepcopy.go` is produced by `controller-gen object:headerFile=hack/boilerplate.go.txt paths=./...`.
CRD YAMLs in `config/crd/` are produced by `controller-gen rbac:roleName=manager-role crd paths=./... output:crd:artifacts:config=config/crd`.
Both are committed and must be regenerated whenever API types change (`task generate`).

### Dependency versions
Pinned to match the account-operator exactly:
- `sigs.k8s.io/controller-runtime v0.23.3`
- `sigs.k8s.io/multicluster-runtime v0.23.1`
- `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go` — all `v0.35.4`
- `go.platform-mesh.io/subroutines v0.3.3`
- `github.com/kcp-dev/multicluster-provider v0.5.1`
