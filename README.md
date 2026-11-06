> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# Platform Mesh

[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/platform-mesh/platform-mesh/badge)](https://scorecard.dev/viewer/?uri=github.com/platform-mesh/platform-mesh)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/12932/badge)](https://www.bestpractices.dev/projects/12932)
[![REUSE status](https://api.reuse.software/badge/github.com/platform-mesh/platform-mesh)](https://api.reuse.software/info/github.com/platform-mesh/platform-mesh)

## Description

Platform Mesh is an open, multi-tenant developer platform built on [kcp](https://github.com/kcp-dev/kcp). It provides account-based tenancy, relationship-based authorization (ReBAC via [OpenFGA](https://openfga.dev/)), and UI extensibility on top of a Kubernetes-like control plane.

This repository is the Platform Mesh **monorepo**: it consolidates the shared libraries, operators, services, and tooling that make up the platform. Each component is its own Go module, linked together through the repository-root [`go.work`](go.work) workspace. The Helm charts (`charts/`), the OCM component definitions (`ocm/`) and the installation guide (`installation/`) live here as well.

## Components

### Libraries

| Module | Description |
|--------|-------------|
| [apis](apis) | Shared CRD Go types, deepcopy, and scheme for the `*.platform-mesh.io` API groups. |
| [golang-commons](golang-commons) | Reusable Go libraries (JWT, context, error handling, logging, controllers) shared across components. |
| [subroutines](subroutines) | Lifecycle engine for Kubernetes/kcp controllers with Result-based flow control, conditions, and observability. |

### Operators

| Operator | Description |
|----------|-------------|
| [account-operator](operators/account-operator) | Manages the core `Account` grouping resource and its owned namespace. |
| [backup-operator](operators/backup-operator) | Orchestrates Velero, CloudNativePG, and etcd-druid to back up and restore a Platform Mesh deployment. |
| [extension-manager-operator](operators/extension-manager-operator) | Lifecycle management of `ContentConfiguration` resources for Micro Frontends. |
| [kcp-migration-operator](operators/kcp-migration-operator) | Synchronizes custom resources from existing Kubernetes clusters into kcp workspaces. |
| [resource-sharding-operator](operators/resource-sharding-operator) | Assigns resources to shards via labels so downstream operator replicas filter their watches by shard. |
| [security-operator](operators/security-operator) | Manages security-related configuration (authorization models, identity providers, tuples, stores, invites). |
| [terminal-controller-manager](operators/terminal-controller-manager) | Manages browser-based terminal sessions (ephemeral pods) to kcp workspaces. |

### Services

| Service | Description |
|---------|-------------|
| [iam-service](services/iam-service) | GraphQL API for user and role management, driving OpenFGA and the identity provider (Keycloak). |
| [virtual-workspaces](services/virtual-workspaces) | Custom kcp-based virtual workspaces that expose tailored data to UIs. |
| [rebac-authz-webhook](services/rebac-authz-webhook) | Kubernetes authorization webhook backed by OpenFGA (ReBAC). |
| [kubernetes-graphql-gateway](services/kubernetes-graphql-gateway) | Exposes Kubernetes resources as a GraphQL API for UIs and tools. |

## Getting started

Each module can be built and tested on its own, or through the repository-root [Taskfile](Taskfile.yaml), which fans out to every component:

```sh
task build      # build all components
task test       # test all components
task lint       # lint all components
task verify     # lint + test
```

Target a single component with the `<target>-<component>` pattern:

```sh
task build-iam-service
task test-account-operator
task images-iam-service VERSION=v0.1.0 PUSH=false
```

The modules are linked through the root [`go.work`](go.work), so cross-module changes resolve against local source during development.

## Development

For the development loop, [Tilt](https://tilt.dev) replaces the OCM/Flux delivery pipeline: the kind installation from the [installation guide](https://platform-mesh.github.io/main/how-to-guides/install-platform-mesh) is rendered straight from [`charts/`](charts), and the operators and services you are working on are compiled on the host and hot-reloaded into their pods in seconds.

`helm` needs to be authenticated with ghcr.io to pull the charts:

```sh
helm registry login ghcr.io --username "<github-user>"
```

Then bring up the kind cluster and tilt:

```sh
task tilt:up  # brings up tilt with a webinterface
task tilt:ci  # bring up tilt until all resources are green, then exits
task tilt:e2e # runs the portal sharded e2e tests against the running tilt PM instance
```

Everything is addressed as `<name>.pm.<node-ip>.sslip.io:31000`; `platformmesh-ready` turning green means the installation is usable. Chart changes are picked up live, service values come from the operator's [kind profile](charts/platform-mesh-operator/files/kind-profile.yaml).

### Validating manifests without deploying

```sh
tilt alpha tiltfile-result -f contrib/tilt/Tiltfile
```

## Releasing

Platform Mesh has one version, `vYY.MM.P` (CalVer: `v26.10.0` is the October 2026 release). Tagging it builds every component image and chart from that commit, publishes the signed OCM component graph and creates a GitHub release; patch releases are tags on `release-YY.MM` branches, backports are plain cherry-picks. See [docs/release/process.md](docs/release/process.md) and [RFC 015](https://github.com/platform-mesh/architecture/blob/main/rfc/015-versioning.md).

## Requirements

Building Platform Mesh requires an installation of Go. Checkout each module's `go.mod` for the required Go version and dependencies.

## Contributing

Please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository for instructions on how to contribute to Platform Mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

## Licensing

Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available via the [REUSE tool](https://api.reuse.software/info/github.com/platform-mesh/platform-mesh).

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
