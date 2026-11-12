## Overview

# Contributing to Platform Mesh

We want to make contributing to this project as easy and transparent as possible.

## Our development process

We use GitHub to track issues and feature requests, as well as accept pull requests.

## Folder layout

Platform Mesh develops in this monorepo, with that a clean folder
structure is important - both for developers to know where to look and
for release engineering to know what to handle with care.

In general Platform Mesh follows the Go conventions.

### root module

```
.
├── cmd/<name>/main.go  # entry point of a binary
├── cmd/<name>/app/     # wiring that the entry point hooks into
├── pkg/<lib>/          # libraries shared across the project
├── hack/               # maintenance and development commands, scripts and assets
├── apis/               # library submodule: API types
├── golang-commons/     # library submodule: shared Go helpers
├── subroutines/        # library submodule: reconciliation subroutines
├── operators/<name>/   # submodule per operator (see Submodules)
├── services/<name>/    # submodule per service (see Submodules)
├── charts/             # Helm charts
├── tests/              # integration and e2e test suites
├── third_party/        # vendored upstream manifests
├── tools/              # shared developer tooling
├── docs/               # deployment and release documentation
├── contrib/            # auxiliary files like the tilt setup
├── installation/       # installation values files
├── ocm/                # component registry and release plumbing
└── bin/                # build output, not tracked
```

`cmd/` contains code for binaries: `cmd/<name>/main.go` is the entry
point and `cmd/<name>/app` the package it hooks into - the command tree,
its flags and the wiring that assembles the implementation. Keep `app`
thin - it should just be wiring, with the implementation itself
belonging in `pkg/`.

`pkg/` contains libraries shared across the project.

`hack/` contains Go commands, shell scripts and auxiliary files for
maintenance and development.
Prefer writing a Go command like `hack/release/release-notes` over shell scripts.

### Submodules

Platform Mesh contains many modules like `operators/platform-mesh-operator`.
Operators that reconcile resources on the control plane are placed in
`operators/`, services that offer an endpoint to users and operators are
placed in `services/`. Both use the directory pattern of the root
module:

```
<module>/                  # operators/<name> or services/<name>
├── go.mod
├── cmd/<name>/main.go     # entry point, calls into app
├── cmd/<name>/app/        # wiring only: commands, flags, assembling the implementation
├── pkg/<lib>/             # implementation, importable by other modules and by tests/
├── internal/<lib>/        # implementation private to the module
├── hack/                  # module-local maintenance and development scripts
└── <assets>/              # non-Go material the module owns, see Assets
```

A module with more than one shipped binary has one directory per binary
under `cmd/`. Development-only binaries are not shipped: put them in
`hack/` of the module that owns them, as a Go command.

**Libraries** (`apis`, `golang-commons`, `subroutines`) are free-form.
They ship no binaries, so they have no `cmd/` and their packages sit
wherever their maintainers judge them easiest to find.

### Assets

Non-Go material stays in the module that owns it, in a directory named
for what it holds. The names below are guidance, not a fixed set:

| Directory | Contents |
|---|---|
| `config/` | kustomize overlays and generated CRDs (`config/crd`, `config/rbac`, ...) |
| `data/` | files a deployed component reads |
| `docs/` | module documentation |
| `examples/` | runnable examples, with the scripts they use |
| `images/` | Docker contexts of a module's additional images |
| `testdata/` | fixtures for Go tests; the `go` tool ignores this directory |

Generator inputs and their tool configs (`*.proto`, `gqlgen.yml`,
`oapi-codegen.yaml`, `PROJECT`) stay at the module root next to the
`Taskfile.yaml` that runs them.

## Pull requests

You are welcome to contribute with your pull requests. These steps explain the contribution process:

1. Fork the repository and create your branch from `main`.
1. [Add tests](#testing) for your code.
1. If you've changed APIs, update the documentation.
1. Make sure the tests pass. Our GitHub Actions pipeline runs the tests for your PR and will indicate
   any issues.
1. Sign the Developer Certificate of Origin (DCO).

## Development setup

Platform Mesh uses [Task](https://taskfile.dev) for build automation:

```bash
task lint       # Format and lint (golangci-lint), every component
task test       # Run the unit tests of every component
task verify     # Run lint, boilerplate and tidy checks
task test-<component>   # one component, e.g. task test-account-operator
```

### Development Setup

Platform Mesh uses [tilt](https://tilt.dev/) to deploy a full Platform
Mesh in kind for development:

```bash
task tilt:up  # deploys the tilt setup and hot-reloads on changes, offers a webui
task tilt:ci  # deploys the tilt setup once and exits
task tilt:e2e # run the e2e tests (tests/e2e Go suites, then the portal suite) against the running tilt instance
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

* Similarly, the dependencies in **libraries** should not be updated unless necessary to be
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

Prefer table-driven tests. There are three tiers:

| Tier | Where | Runs against | Command |
|---|---|---|---|
| unit | next to the code | nothing external | `task test`, `task test-<component>` |
| integration | `tests/integration/<area>` | a kcp in Docker (kcp-testcontainers), components in-process | `task test-integration` |
| e2e | `tests/e2e/<area>` | a deployed Platform Mesh (`KUBECONFIG`, `ADMIN_KUBECONFIG`), usually Tilt | `task test-e2e` |

```bash
task test                                           # unit tests, every component
task test-integration                               # all integration tests
task tests:integration PKG=./integration/account/...  # one integration area
task tilt:ci && task test-e2e                       # e2e against a fresh Tilt environment
```

A unit test must not start a process (kcp, kube-apiserver, kind, docker). A test that needs a
control plane goes into `tests/integration` (and wires the component through its public
packages under `pkg/`, not `cmd/`) or, if it needs a deployed stack, into
`tests/e2e`. See
[tests/README.md](tests/README.md) for the framework and how to add a package.

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
