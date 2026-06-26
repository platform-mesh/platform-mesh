# Platform Mesh - apis

## Description

`apis` is the shared API module for Platform Mesh. It holds the Go type definitions, deepcopy, and registration scheme for the Custom Resource Definitions that make up the Platform Mesh control plane on [kcp](https://github.com/kcp-dev/kcp).

The operators and services in this monorepo import these types instead of redefining them, so the API surface stays consistent across the platform.

## API Groups

| Group | Version | Kinds |
|-------|---------|-------|
| `core.platform-mesh.io` | `v1alpha1` | `Account`, `AccountInfo`, `Store`, `Invite`, `AuthorizationModel`, `APIExportPolicy`, `IdentityProviderConfiguration` |
| `ui.platform-mesh.io` | `v1alpha1` | `ProviderMetadata`, `ContentConfiguration` |
| `gateway.platform-mesh.io` | `v1alpha1` | `ClusterMetadata`, `ClusterAccess` |
| `marketplace.platform-mesh.io` | `v1alpha1` | `MarketplaceEntry` |
| `terminal.platform-mesh.io` | `v1alpha1` | `Terminal` |
| `sharding.platform-mesh.io` | `v1alpha1` | `ResourceSharding` |
| `search.platform-mesh.io` | `v1alpha1` | `SearchIndex` |
| `backup.platform-mesh.io` | `v1alpha1` | `PlatformBackup`, `PlatformRestore` |
| `migration.platform-mesh.io` | `v1alpha1` | `KcpMigration` |

## Getting started

Add the dependency to your Go module:

```
go get go.platform-mesh.io/apis
```

Import the types and register them with a scheme:

```go
import (
    corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
    "k8s.io/apimachinery/pkg/runtime"
)

scheme := runtime.NewScheme()
_ = corev1alpha1.AddToScheme(scheme)
```

## Requirements

`apis` requires an installation of Go. Checkout the [go.mod](go.mod) for the required Go version and dependencies.

## Contributing

Please refer to the [CONTRIBUTING.md](../CONTRIBUTING.md) file in this repository for instructions on how to contribute to Platform Mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
