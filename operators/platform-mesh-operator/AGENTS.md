## Repository Description
- `platform-mesh-operator` bootstraps and reconciles Platform Mesh installations through the `PlatformMesh` custom resource.
- `PlatformMesh` is the top-level installation resource. It drives deployment, KCP setup, provider secrets, feature toggles, and related bootstrap state.
- This is a Go operator repo built around [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime), [multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime), and [Open Component Model](https://github.com/open-component-model/ocm).
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. Prefer the narrowest fix that addresses the real problem.
- Verify behavior before finishing. Start with targeted tests, then broader validation if needed.
- Prefer existing repo workflows and `task` targets over ad-hoc commands.
- Keep this file focused on agent execution; use `README.md` for installation and domain context.

## Project Structure
- `api/v1alpha1`: `PlatformMesh` API types and generated deepcopy code.
- `internal/controller`: reconciler entrypoints and controller tests.
- `internal/config`: runtime configuration.
- `pkg/subroutines`: installation and bootstrap subroutines such as deployment, KCP setup, defaults, waiting, and provider secrets.
- `pkg/kapply`, `pkg/merge`, `pkg/ocm`: supporting helpers for applying manifests, merging values, and OCM integration.
- `config/`: operator deployment manifests, CRDs, RBAC, and local runtime config.
- `manifests/k8s`, `manifests/kcp`, `manifests/features`: templated or curated installation assets applied by the operator. All three are embedded into the binary: an `embed.go` per directory exposes a `Resources` `embed.FS`, so a manifest change needs a rebuild. `gotemplates/` is embedded the same way through its single `gotemplates/embed.go`; the operator reads nothing from disk at runtime.
- `test/e2e/kind`: kind-based end-to-end tests.
- `kind-config.yaml`: local kind setup for e2e scenarios.

## Architecture
This operator is the bootstrap and installation orchestrator for Platform Mesh. Most changes affect the full platform bring-up path, not just a single controller.

### Runtime model
- The manager is a standard controller-runtime manager, not a multicluster-runtime manager.
- `cmd/operator.go` starts the operator with `platformmeshcontext.StartContext(...)`, controller-runtime logging, and a single `PlatformMesh` reconciler.
- Local development uses `.env` through `task run`; tests and e2e flows rely on kind, mkcert, and local helper binaries from `bin/`.

### Reconciliation model
- The core reconciler lives under `internal/controller` and delegates most real work into subroutines in `pkg/subroutines`.
- The important behavior is not just CRUD on the `PlatformMesh` object; reconciliation drives installation state across Kubernetes, KCP, OCM, Flux resources, webhook secrets, and feature-specific manifests.
- Changes to subroutine ordering or shared helper behavior can affect the whole bootstrap pipeline.

### Domain model
- `PlatformMesh` is the top-level installation contract. Its spec controls exposure, OCM references, KCP connections, provider secrets, extra workspaces, feature toggles, and values passed into deployed components.
- Files under `manifests/k8s`, `manifests/kcp`, and `manifests/features` are not all “generated output”; many are maintained installation assets consumed by the operator at runtime.
- Feature toggles and manifest selections are part of the product behavior, not just deployment details.

### Testing and local execution
- `task test` runs the main local test flow and uses kind + mkcert setup.
- `task kindtest` runs the dedicated kind e2e suite under `test/e2e/kind`.
- `task cover` enforces thresholds from `.testcoverage.yml`.
- If tests fail in CI, the Taskfile already contains extra diagnostics for HelmReleases, pods, OpenFGA, and related bootstrap resources.

## Commands
- `task fmt` — format Go code.
- `task lint` — run formatting plus golangci-lint.
- `task test` — run the standard local test flow.
- `task kindtest` — run the kind-specific e2e test suite.
- `task cover` — enforce coverage thresholds from `.testcoverage.yml`.
- `task validate` — run lint and tests together.
- `task manifests` — regenerate CRDs.
- `task generate` — regenerate CRDs and deepcopy code after API changes.
- `task mockery` — regenerate mocks when interfaces change.
- `task build` — build the manager binary.
- `task run` — run the operator locally using `.env`.
- `task docker-build` — build the container image.
- `task docker:kind` — build, load, and restart the deployment in kind.

## Code Conventions
- Follow existing operator and subroutine patterns before introducing new abstractions.
- Keep reconciliation flow in `internal/controller`; put reusable install logic in `pkg/subroutines`.
- Add or update `_test.go` files when behavior changes.
- When editing API types under `api/v1alpha1`, regenerate derived files instead of hand-editing generated output.
- Treat changes under `manifests/` as high-impact because they affect installation and bootstrap behavior.
- Keep logs structured and avoid logging secrets, kubeconfigs, or generated credentials.

## Generated Artifacts
- Run `task generate` after changing API types or CRD shape.
- Run `task mockery` after interface changes that affect generated mocks.
- Review generated changes separately from manual logic changes when possible.
- Do not hand-edit generated output unless the file is clearly maintained as source.

## Do Not
- Edit `api/v1alpha1/zz_generated.deepcopy.go` by hand.
- Update `.testcoverage.yml` unless the task explicitly requires it.
- Treat files under `manifests/` as disposable generated output; many are maintained installation assets and should be reviewed carefully.
- Skip regeneration after changing `PlatformMesh` API types.

## Hard Boundaries
- Do not invent new local workflows when a `task` target already exists.
- Ask before changing release flow, CI wiring, published image behavior, or Helm/chart integration outside this repo.
- Be careful with bootstrap and installation changes; small manifest edits can affect the whole platform bring-up path.

## Human-Facing Guidance
- Use `README.md` for local certificate setup, startup arguments, and service context.
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.
