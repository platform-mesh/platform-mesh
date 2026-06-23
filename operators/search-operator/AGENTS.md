## Repository Description
- `search-operator` manages `SearchIndex` resources and indexes searchable workspace data into OpenSearch across kcp workspaces.
- The main moving parts are the multicluster manager, the `SearchIndex` lifecycle reconciler, and the per-resource indexing controllers created for configured searchable GVKs.
- This is a Go operator built around [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime), [multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime), and [multicluster-provider](https://github.com/kcp-dev/multicluster-provider).
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. Indexing behavior spans kcp, controller lifecycle logic, and OpenSearch writes.
- Prefer existing controller and lifecycle patterns over introducing a second reconciliation style.
- Verify behavior before finishing. Start with focused tests, then broader validation if needed.
- Keep this file focused on agent execution and repository-specific constraints.

## Project Structure
- `api/v1alpha1`: API types and generated deepcopy code for `SearchIndex`.
- `cmd`: operator startup and manager wiring.
- `internal/controller`: reconciler setup for `SearchIndex` and indexable resources.
- `internal/subroutine`: lifecycle subroutines that implement indexing behavior.
- `internal/config`: environment-driven operator configuration.
- `internal/opensearch`: OpenSearch client integration.
- `config`: CRDs, RBAC, manifests, and generated API resources.
- `scripts`: manual testing helpers.

## Architecture
This is not a plain single-cluster controller-runtime operator.

### Runtime model
- `cmd/main.go` creates an `mcmanager.Manager` from `sigs.k8s.io/multicluster-runtime`, backed by `apiexport.New(...)` from `kcp-dev/multicluster-provider`.
- The operator watches across workspaces exposed through the configured APIExport endpoint slice, not just the local cluster.
- OpenSearch connectivity is required for the indexing path; startup currently exits if client creation or ping fails.

### Reconciliation model
- `SearchIndexReconciler` delegates to the shared lifecycle manager from `platform-mesh/golang-commons/controller/lifecycle`.
- The `SearchIndex` lifecycle is implemented through subroutines in `internal/subroutine`, not ad-hoc reconcile logic.
- Additional controllers are created for every configured searchable GVK so indexed resources can be observed across workspaces.

### Domain model
- `SearchIndex` describes the logical search index configuration used by the service side.
- Indexed workspace resources are transformed into OpenSearch documents, which are then consumed by the `search` service.

## Commands
- `task fmt` — format Go code.
- `task lint` — run formatting plus golangci-lint.
- `task test` — run the standard local test flow with envtest and kcp tooling.
- `task cover` — run tests with coverage output.
- `task validate` — run the standard validation flow.
- `task manifests` — regenerate CRDs.
- `task generate` — regenerate deepcopy code and API resource output.
- `task run` — run the operator locally.
- `task build` — compile all packages.

## Code Conventions
- Keep controller wiring in `internal/controller` and indexing behavior in `internal/subroutine`.
- Add or update `_test.go` files together with behavior changes.
- When editing API types under `api/v1alpha1`, regenerate derived files instead of hand-editing generated output.
- Keep logging structured and avoid logging credentials or raw secrets.

## Generated Artifacts
- If API types or CRD schemas change, run `task generate`.
- Review generated updates in `config/` separately from manual logic changes when possible.
- Do not hand-edit `api/v1alpha1/zz_generated.deepcopy.go`.

## Do Not
- Edit generated API files manually.
- Treat indexing behavior, kcp watches, or OpenSearch writes as low-risk local-only changes.
- Update `.testcoverage.yml` unless the task explicitly requires it.

## Hard Boundaries
- Do not invent new local workflows when a `task` target already exists.
- Ask before making backward-incompatible API or indexing-contract changes that require coordination with the `search` service or downstream consumers.

## Human-Facing Guidance
- Use `README.md` for local setup and rough architecture context.
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.
