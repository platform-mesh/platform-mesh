## Repository Description
- `terminal-controller-manager` manages browser-based terminal sessions for kcp workspaces through a Kubernetes controller and terminal pod image.
- `Terminal` resources drive ephemeral terminal pods that provide workspace access from the browser. The repo also contains the terminal image under `images/terminal`.
- This is a Go service built around [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime), [multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime), and generated Kubernetes APIs.
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. Prefer the simplest fix that addresses the root cause.
- Verify the affected path before finishing. Start with the smallest relevant command.
- Prefer existing `task` targets over ad-hoc shell commands.
- Keep human-facing process details in `CONTRIBUTING.md`; keep this file focused on agent execution.

## Project Structure
- `api/v1alpha1`: API types for terminal resources.
- `internal/controller`: reconciliation logic.
- `internal/config`: runtime configuration.
- `pkg/subroutines`: reusable reconciliation helpers.
- `config/crd`, `config/resources`: generated Kubernetes and API resources.
- `images/terminal`: terminal pod image sources and Docker context.
- `docs/CONCEPT.md`: architecture and design context.
- `test-ui`: local browser UI for websocket testing.

## Architecture
This service splits responsibility between kcp-backed `Terminal` resources and runtime-cluster infrastructure objects such as pods, services, and HTTPRoutes.

### Runtime model
- `cmd/operator.go` builds a multicluster manager against kcp using an APIExport provider and a separate runtime-cluster client for pod/service/route management.
- `--kcp-kubeconfig` controls the kcp watch config. The runtime cluster still comes from the standard controller-runtime config.
- Leader election and manager lifecycle are local to the runtime cluster, while `Terminal` reconciliation happens through multicluster-runtime.

### Reconciliation model
- `TerminalReconciler` is lifecycle-driven and uses `mcreconcile.Request`, `mcbuilder.ControllerManagedBy(mgr)`, and `conditions.NewManager()`.
- The reconciliation chain is assembled from subroutines in a fixed order: lifetime, pod, service, then HTTPRoute, each gated by config flags.
- Cache sync period is tied to `terminal-lifetime`, so lifetime changes affect reconciliation behavior directly.

### Domain model
- `Terminal` resources represent browser terminal sessions in kcp workspaces.
- The operator creates and maintains the runtime infrastructure needed to expose a terminal session: pod, service, and HTTPRoute.
- The repo also owns the terminal image under `images/terminal`, but controller changes and image changes should stay separate unless the task truly spans both.

### Configuration and tests
- `internal/config/config.go` defines toggles for each subroutine plus kcp, terminal, and gateway flags.
- Tests are split between normal Go tests and browser/websocket-oriented local checks using `test-ui`.

## Commands
- `task build` — build the manager binary.
- `task run` — run the controller locally.
- `task fmt` — format Go code.
- `task lint` — run formatting plus golangci-lint.
- `task envtest` — run Go tests.
- `task test` — run the standard local test flow.
- `task validate` — run lint and tests together.
- `task manifests` — regenerate CRDs.
- `task generate` — regenerate deepcopy code and API resources.
- `task docker-build` — build the controller image.
- `task docker-terminal` — build the terminal pod image.
- `task docker-terminal:kind` — load the terminal image into kind.
- `task docker:kind` — build, load, and restart the controller deployment in kind.
- `task test-ui` — serve the local websocket test UI.

## Code Conventions
- Follow existing controller-runtime and package patterns before introducing new abstractions.
- Keep controller logic in `internal/controller`; place reusable helpers in `pkg/subroutines`.
- Update or add `_test.go` files when changing behavior.
- When editing API types, regenerate derived artifacts instead of hand-editing generated output.
- Never edit generated files manually unless the repo already treats them as source.
- Keep logs structured and avoid logging secrets, bearer tokens, or kubeconfig contents.

## Generated Artifacts
- Run `task generate` after changing API types or CRD shape.
- Review changes in `config/crd` and `config/resources`.
- Keep generated changes separate from unrelated manual edits when possible.

## Do Not
- Edit generated CRD or resource output in `config/crd` or `config/resources` without running `task generate`.
- Change both the controller and terminal image behavior unless the task actually requires both.

## Hard Boundaries
- Do not invent new local workflows when a `task` target already exists.
- Do not change both the controller and terminal image behavior unless the task actually requires both.
- Ask before changing release flow, CI wiring, published image names, or Helm integration outside this repo.

## Human-Facing Guidance
- Use `README.md` for local setup and high-level context.
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.
