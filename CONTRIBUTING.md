## Overview

# Contributing to Platform Mesh

We want to make contributing to this project as easy and transparent as possible.

## Our development process

We use GitHub to track issues and feature requests, as well as accept pull requests.

## Pull requests

You are welcome to contribute with your pull requests. These steps explain the contribution process:

1. Fork the repository and create your branch from `main`.
1. [Add tests](#testing) for your code.
1. If you've changed APIs, update the documentation.
1. Make sure the tests pass. Our GitHub Actions pipeline runs the tests for your PR and will indicate
   any issues.
1. Sign the Developer Certificate of Origin (DCO).

## Development setup

subroutines uses [Task](https://taskfile.dev) for build automation:

```bash
task lint       # Format and lint (golangci-lint)
task test       # Run unit tests
task cover      # Run tests with coverage
task validate   # Run lint + test
```

### Code style

- Follow the [gci import ordering](https://github.com/daixiang0/gci): standard, default, `k8s.io` prefix
- Avoid named return values
- Use `ptr.To()` and `ptr.Deref()` from `k8s.io/utils/ptr`
- Use `any` instead of `interface{}`

### Go Version Management

* The `go` directive in the `go.mod` / `go.work` files declare the _minimum required_ version of Go
  to build Platform Mesh. This is notably **not the same version Platform Mesh is currently built with.**
  When bumping Go, this directive **must not** be updated just for the sake of "consistency". Only
  a `go mod tidy` or `go work sync` should ever change these directives.

  Bumping these needlessly to either the latest minor or, even worse, every single patch release of
  Go, is just causing unnecessary havoc for everyone downstream.

  To actually update the Go version used for Platform Mesh, adjust the various `Dockerfiles`.

* Similarily, the dependencies in **libraries** should not be updated unless necessary to be
  compatible with the modules of the operators/services. The libraries like `golang-commons` or `apis`
  are meant to provide the broadest possible compatibility, making them as easy to integrate as
  possible.

  This means CVEs that only affect our libraries **must not be fixed inside those libraries**, but
  instead in the various operator/services (the binaries we produce) `go.mod` files. The same logic
  as for the `go` directive applies here: Just because we consider a CVE to be important does not
  mean we have to force downstream consumers to also upgrade. They might have an entirely different
  set of conditions, maybe a regulated environment, where triaging these issues is done differently.

  To sum up: `go.mod` files are not ways to communicate CVE and their remedies to downstream consumers.

## Testing

> **NOTE:** You should always add tests if you are adding code to our repository.

Prefer table-driven tests. To run tests locally:

```bash
task test
```

## Generative AI

If you use generative AI tools while preparing a contribution, you remain responsible for the
correctness, safety, licensing, and maintainability of the submitted change.

If you use Claude while contributing, do not assume it will automatically pick up this repository's
`AGENTS.md`. Explicitly provide or reference `AGENTS.md` at the start of the session so Claude has
the repository-specific instructions before it suggests or applies changes.

## Issues

We use GitHub issues to track bugs. Please ensure your description is clear and includes sufficient
instructions to reproduce the issue.

## License

By contributing to Platform Mesh, you agree that your contributions will be licensed under its
[Apache-2.0 license](LICENSE).
