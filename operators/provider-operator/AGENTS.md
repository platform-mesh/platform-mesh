## Repository Description
- `provider-operator` reconciles `ManagedProvider` and `Provider` resources (API group `providers.platform-mesh.io`, types in `go.platform-mesh.io/apis/providers/v1alpha1`).
- `ManagedProvider` lives on the runtime cluster. Its reconciler creates the matching `Provider` in kcp, waits for it, copies the provider's scoped kubeconfig to the runtime cluster and deploys the provider's Flux components.
- `Provider` lives in kcp and is watched through the providers APIExport. Its reconciler creates the provider's workspace under `root:providers` and a kubeconfig scoped to that workspace.
- This is a Go operator built around [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime), [multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime), and [multicluster-provider](https://github.com/kcp-dev/multicluster-provider).
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Status
- Scaffold only. Both controllers still live in `platform-mesh-operator` (`internal/controller/providers`, `pkg/subroutines/providers`) and move here next.
- Until the cutover, `platform-mesh-operator` runs the controllers in production. Never run both against the same cluster.

## Core Principles
- Keep changes small and local. Provider onboarding spans the runtime cluster, kcp workspaces and Flux deployments.
- Prefer the existing lifecycle and subroutine patterns over introducing a second reconciliation style.
- Verify behavior before finishing. Start with focused tests, then broader validation if needed.
- Keep this file focused on agent execution and repository-specific constraints.

## Project Structure
- `main.go`: entry point, calls `cmd.Execute()`.
- `cmd`: cobra root command, common flags and the `operator` subcommand that wires the manager.
- `config`: kustomize manifests for local use (manager, RBAC, metrics). Production deploys through the Helm chart.

## Architecture
This is not a plain single-cluster controller-runtime operator.

### Runtime model
- `cmd/operator.go` creates an `mcmanager.Manager` from `sigs.k8s.io/multicluster-runtime` with the `multi` provider.
- `ManagedProvider` is reconciled on the local (runtime) cluster; `Provider` is reconciled in the kcp workspaces the providers APIExport endpoint slice exposes, through an `apiexport` provider added to the `multi` provider.
- Every controller sets `WithEngageWithLocalCluster` and/or `WithEngageWithProviderClusters` in `For()`, so it only receives events from the clusters it is meant for.

### Reconciliation model
- Reconcilers delegate to the shared lifecycle manager from `go.platform-mesh.io/subroutines`; behavior lives in subroutines, not in ad-hoc reconcile logic.

## Commands
Run from this directory:
- `task fmt`: format Go code.
- `task lint`: run formatting plus golangci-lint with the root `.golangci.yaml`.
- `task test`: run unit tests.
- `task cover`: run tests with coverage output.
- `task build`: compile all packages.
- `task docker-build`: build the image (build context is the repository root).

Run from the repository root:
- `task verify-provider-operator`: lint, `go.mod` tidiness and license headers, as CI runs them.

## Code Conventions
- Keep controller wiring in the controller package and provider behavior in subroutines.
- Add or update `_test.go` files together with behavior changes.
- Keep logging structured and never log kubeconfigs, tokens or other secret contents.
- Require published versions of `go.platform-mesh.io/apis`, `golang-commons` and `subroutines` in `go.mod`; do not add `replace` directives for them.

## Do Not
- Edit generated files by hand.
- Reuse `platform-mesh-operator`'s leader election ID; both operators run in the same namespace.
- Treat changes to provider workspaces, scoped kubeconfigs or Flux deployments as low-risk local-only changes.

## Hard Boundaries
- Do not invent new local workflows when a `task` target already exists.
- Ask before changing the `providers.platform-mesh.io` API contract; providers and other operators depend on it.

## Human-Facing Guidance
- Use `README.md` for local setup and rough architecture context.
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.
