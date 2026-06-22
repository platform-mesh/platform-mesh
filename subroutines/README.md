# Platform Mesh - subroutines

[![OpenSSF Scorecard](https://api.scorecard.dev/projects/go.platform-mesh.io/subroutines/badge)](https://scorecard.dev/viewer/?uri=go.platform-mesh.io/subroutines)
[![CI](https://go.platform-mesh.io/subroutines/actions/workflows/ci.yml/badge.svg)](https://go.platform-mesh.io/subroutines/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/go.platform-mesh.io/subroutines.svg)](https://pkg.go.dev/go.platform-mesh.io/subroutines)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## Description

subroutines is a lifecycle engine for Kubernetes controllers built on [multicluster-runtime](https://github.com/platform-mesh/multicluster-runtime). It offers explicit Result-based flow control, opt-in condition/status management, and built-in observability.

- **Subroutine orchestration** — chain subroutines with typed `Result` values (`OK`, `Pending`, `Stop`) instead of plain errors
- **Condition management** — automatic per-subroutine and aggregate `Ready` conditions
- **Spread scheduling** — distribute reconciliation load over configurable time windows to avoid thundering-herd effects
- **Finalizer lifecycle** — declarative finalizer registration, ordered teardown, and initializer/terminator support
- **Observability** — built-in Prometheus metrics (duration, errors, requeue) and OpenTelemetry tracing

## Getting started

Add the dependency to your Go module:

```
go get go.platform-mesh.io/subroutines
```

### Quick start

```go
import (
    "go.platform-mesh.io/subroutines/conditions"
    "go.platform-mesh.io/subroutines/lifecycle"
)

lc := lifecycle.New(mgr, "MyController", func() client.Object { return &v1alpha1.MyResource{} },
    subroutine1,
    subroutine2,
).WithConditions(conditions.NewManager())
```

Each subroutine implements one or more of the core interfaces:

```go
// Processor handles the main reconciliation logic.
type Processor interface {
    Subroutine
    Process(ctx context.Context, obj client.Object) (Result, error)
}

// Finalizer handles cleanup when an object is being deleted.
type Finalizer interface {
    Subroutine
    Finalize(ctx context.Context, obj client.Object) (Result, error)
    Finalizers(obj client.Object) []string
}
```

Subroutines return a `Result` to control the chain:

```go
subroutines.OK()                          // continue, no requeue
subroutines.OKWithRequeue(5 * time.Minute) // continue, requeue after duration
subroutines.Pending(30 * time.Second, "waiting for dependency") // continue, set condition to Unknown
subroutines.StopWithRequeue(time.Minute, "rate limited")        // stop chain, requeue
subroutines.Stop("done")                                        // stop chain, no requeue
```

## Packages

| Package | Description |
|---------|-------------|
| [`subroutines`](https://pkg.go.dev/go.platform-mesh.io/subroutines) | Core interfaces (`Subroutine`, `Processor`, `Finalizer`, `Initializer`, `Terminator`) and `Result` type |
| [`lifecycle`](https://pkg.go.dev/go.platform-mesh.io/subroutines/lifecycle) | Orchestration engine — executes subroutines, manages finalizers, patches status |
| [`conditions`](https://pkg.go.dev/go.platform-mesh.io/subroutines/conditions) | Per-subroutine and aggregate `Ready` condition management |
| [`spread`](https://pkg.go.dev/go.platform-mesh.io/subroutines/spread) | Reconciliation spreading to distribute load over time |
| [`metrics`](https://pkg.go.dev/go.platform-mesh.io/subroutines/metrics) | Prometheus metrics for subroutine execution |

## Requirements

subroutines requires Go. Check the [go.mod](go.mod) for the required Go version.

## Contributing

Please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository for instructions on how to contribute to Platform Mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
