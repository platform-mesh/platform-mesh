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
