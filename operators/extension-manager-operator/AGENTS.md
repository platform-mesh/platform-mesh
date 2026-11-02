## Repository Description
- `extension-manager-operator` manages the lifecycle of `ContentConfiguration` resources for Platform Mesh.
- `ContentConfiguration` defines micro frontend content and related runtime validation behavior used by Platform Mesh.
- This is a Go operator repo built around [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime), [multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime), and generated Kubernetes APIs.
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. Prefer the narrowest fix that solves the real problem.
- Verify behavior before finishing. Start with targeted tests, then broader validation when needed.
- Prefer existing repo workflows and `task` targets over custom command sequences.
- Keep human-facing process details in `CONTRIBUTING.md`.

## Project Structure
- `api/v1alpha1`: API types and generated Kubernetes objects.
- `internal/controller`: reconcilers and controller tests.
- `internal/server`: runtime server components.
- `internal/config`: runtime configuration parsing and defaults.
- `pkg/validation`: content configuration validation logic.
- `pkg/transformer`: schema and transformation helpers.
- `pkg/subroutines`, `pkg/util`: reusable operator helpers.
- `config/crd`, `config/resources`: generated manifests. The chart copy of the CRD is written by `hack/charts/sync-crds.sh` as part of `task generate`.
- `docs` and `docs/development`: product and developer documentation.

## Architecture
This is a multicluster `ContentConfiguration` operator with a thin controller and most business logic delegated to a lifecycle subroutine.

### Runtime model
- `cmd/operator.go` chooses the reconcile config from `KUBECONFIG` if present, otherwise `ctrl.GetConfigOrDie()`.
- If `--kcp-api-export-endpoint-slice-name` is set, the manager is a multicluster manager backed by an APIExport provider; otherwise it reconciles against plain Kubernetes.
- Leader election, when enabled, uses `rest.InClusterConfig()` separately from the reconcile config.

### Reconciliation model
- `ContentConfigurationReconciler` uses `mcreconcile.Request`, `mcbuilder.ControllerManagedBy(mgr)`, and `lifecycle.Lifecycle.Reconcile`.
- The controller is intentionally thin: the main behavior lives in `pkg/subroutines` via `NewContentConfigurationSubroutine(...)`.
- Conditions are managed by `conditions.NewManager()` wired into the lifecycle; avoid setting them ad hoc.

### Domain model
- `ContentConfiguration` is the core API. Validation and transformation logic live outside the controller in `pkg/validation` and `pkg/transformer`.
- Runtime HTTP/server behavior lives under `internal/server`, so not every change belongs in the reconciler path.

### Configuration and tests
- `internal/config/config.go` defines the operator flags, including whether the content configuration subroutine is enabled.
- Controller tests use envtest; generated artifacts under `config/resources` and CRD output must stay in sync with API changes.

## Commands
- `task fmt` — format Go code.
- `task lint` — run formatting plus golangci-lint.
- `task envtest` — run tests with envtest assets.
- `task test` — run the standard local test flow.
- `task cover` — enforce coverage checks using `.testcoverage.yml`.
- `task validate` — run lint, test, and coverage together.
- `task manifests` — regenerate CRDs.
- `task schemagen` — run `go generate` helpers.
- `task generate` — regenerate Kubernetes objects and API resources.
- `task mockery` — refresh mocks when interfaces change.
- `task run` — run the operator locally.
- `task docker-build` — build the container image.
- `task docker:kind` — build, load, and restart operator deployments in kind.

## Code Conventions
- Follow existing package boundaries before introducing new abstractions.
- Keep controller concerns in `internal/controller`, server concerns in `internal/server`, and reusable validation logic in `pkg`.
- Add or update `_test.go` files next to the touched code.
- When interfaces change, regenerate mocks instead of hand-editing generated mock files.
- When API types or CRD shape changes, regenerate artifacts instead of editing generated output manually.
- Keep logs structured and avoid logging secrets, credentials, or full remote content payloads unless required.

## Generated Artifacts
- Run `task generate` after API, schema, or CRD changes.
- Run `task mockery` after interface changes that affect generated mocks.
- Review changes in `config/crd`, `config/resources`, and generated Go files.
- Do not hand-edit generated output unless the file is clearly maintained as source.

## Do Not
- Edit generated CRD or resource output in `config/crd` or `config/resources` without regeneration.
- Hand-edit generated mocks after interface changes; run `task mockery` instead.
- Update `.testcoverage.yml` unless the task explicitly requires it.

## Hard Boundaries
- Do not invent new validation or test workflows when a `task` target already exists.
- Ask before changing release flow, CI wiring, published images, or Helm/chart integration outside this repo.
- Keep generated and manual edits easy to review; separate them when feasible.

## Human-Facing Guidance
- Use `README.md` for local setup and high-level context.
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.
