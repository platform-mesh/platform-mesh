> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# Platform Mesh

[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/platform-mesh/platform-mesh/badge)](https://scorecard.dev/viewer/?uri=github.com/platform-mesh/platform-mesh)
[![REUSE status](https://api.reuse.software/badge/github.com/platform-mesh/platform-mesh)](https://api.reuse.software/info/github.com/platform-mesh/platform-mesh)

## Description

Platform Mesh is an open, multi-tenant developer platform built on [kcp](https://github.com/kcp-dev/kcp). It provides account-based tenancy, relationship-based authorization (ReBAC via [OpenFGA](https://openfga.dev/)), search, and UI extensibility on top of a Kubernetes-like control plane.

This repository is the Platform Mesh **monorepo**: it consolidates the shared libraries, operators, services, and tooling that make up the platform. Each component is its own Go module, linked together through the repository-root [`go.work`](go.work) workspace, and released independently via component-scoped tags.

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
| [search-operator](operators/search-operator) | Indexes kcp workspaces and accounts into OpenSearch together with OpenFGA permission tuples. |
| [security-operator](operators/security-operator) | Manages security-related configuration (authorization models, identity providers, tuples, stores, invites). |
| [terminal-controller-manager](operators/terminal-controller-manager) | Manages browser-based terminal sessions (ephemeral pods) to kcp workspaces. |

### Services

| Service | Description |
|---------|-------------|
| [iam-service](services/iam-service) | GraphQL API for user and role management, driving OpenFGA and the identity provider (Keycloak). |
| [search-service](services/search-service) | REST API to query OpenSearch-indexed resources with OpenFGA post-filtering. |
| [virtual-workspaces](services/virtual-workspaces) | Custom kcp-based virtual workspaces that expose tailored data to UIs. |
| [rebac-authz-webhook](services/rebac-authz-webhook) | Kubernetes authorization webhook backed by OpenFGA (ReBAC). |
| [kubernetes-graphql-gateway](services/kubernetes-graphql-gateway) | Exposes Kubernetes resources as a GraphQL API for UIs and tools. |

### Tooling

Repository tooling lives under [`cmd/`](cmd) — including [`release`](cmd/release), which cuts component-scoped release tags for the monorepo.

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
task images-search-service VERSION=v0.1.0 PUSH=false
```

The modules are linked through the root [`go.work`](go.work), so cross-module changes resolve against local source during development.

## Releasing

Releases are component-scoped: tagging `<component>/vX.Y.Z` triggers that component's GitHub Actions workflow to build and sign its image, cut a GitHub release, bump its chart, and publish a signed OCM component. The [`release`](cmd/release) tool (`task release`) manages the tag registry and ordering.

## Requirements

Building Platform Mesh requires an installation of Go. Checkout each module's `go.mod` for the required Go version and dependencies.

## Contributing

Please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository for instructions on how to contribute to Platform Mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

## Licensing

Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available via the [REUSE tool](https://api.reuse.software/info/github.com/platform-mesh/platform-mesh).

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
