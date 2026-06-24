## Repository Description
- `rebac-authz-webhook` is a Kubernetes authorization webhook for Platform Mesh backed by OpenFGA.
- It evaluates Kubernetes authorization requests and routes them through Platform Mesh-specific authorization handlers and kcp-aware discovery.
- This is a Go webhook service built around [OpenFGA](https://github.com/openfga/openfga), [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime), and [multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime).
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. Prefer the simplest fix that addresses the root cause.
- Verify behavior before finishing. Start with focused package tests.
- Prefer existing `task` targets over ad-hoc shell commands.
- Keep human-facing process details in `CONTRIBUTING.md`.

## Project Structure
- `cmd`: CLI and webhook startup entrypoints.
- `pkg/authorization`: top-level webhook authorization flow.
- `pkg/handler`: request handlers for contextual, org, and non-resource authorization paths.
- `pkg/clustercache`: kcp cluster and discovery cache behavior.
- `pkg/config`: runtime configuration.
- `pkg/retry`, `pkg/util`: shared helpers.
- `config/authz.yaml`: local authorization webhook config.
- `kind.yaml`: local kind setup for development and debugging.

## Architecture
This service is an authorization webhook. The main runtime path is: receive `SubjectAccessReview`, choose the right handler branch, and resolve authorization context through OpenFGA plus kcp-derived cluster metadata.

### Runtime model
- `cmd/serve.go` creates an `mcmanager.Manager` backed by an APIExport provider, but the manager is primarily used as a webhook host plus a source of multicluster access.
- The webhook server registers a single `/authz` endpoint and adds health/readiness checks.
- Startup resolves the OpenFGA `orgs` store, root kcp cluster client, orgs cluster id, and a long-lived cluster cache before serving traffic.

### Authorization model
- `pkg/authorization/webhook.go` only handles HTTP decoding/encoding of `SubjectAccessReview`; authorization decisions come from composed handlers.
- Handler composition in `cmd/serve.go` uses `union.New(...)` over non-resource, org-scoped, and contextual handlers.
- The contextual path depends on `pkg/clustercache`, which maps cluster names to store id, account name, parent cluster id, and a REST mapper.

### kcp and cache model
- `pkg/clustercache` rewrites the root config to `/clusters/root:orgs`, loads org `Store` resources, and builds a dynamic REST mapper per engaged cluster.
- Cache misses are retried through an expiring retry tracker; changes here directly affect webhook behavior under new or still-initializing clusters.

### Configuration and tests
- Most runtime behavior is flag-driven from `pkg/config`, including endpoint slice name, allowed non-resource prefixes, retry timing, and webhook TLS settings.
- Focused package tests are important because broad behavior changes can alter every Kubernetes authorization decision.

## Commands
- `task fmt` — format code with golangci-lint formatting.
- `task lint` — run formatting plus golangci-lint.
- `task test` — run Go tests.
- `task cover` — run tests with coverage output.
- `task assert-coverage` — enforce thresholds from `.testcoverage.yml`.
- `task validate` — run lint and tests together.
- `task mockery` — regenerate mocks when interfaces change.
- `task docker-build` — build the container image.
- `task docker:kind` — build, load, and restart the deployment in kind.
- `go test ./...` — fast fallback for targeted verification.

## Code Conventions
- Follow the existing handler split before introducing new authorization abstractions.
- Keep request-path-specific logic in the corresponding `pkg/handler/*` package.
- Add or update `_test.go` files next to changed behavior.
- Regenerate mocks instead of hand-editing generated mock files.
- Be careful with kcp discovery and cache behavior; small changes can affect every authorization request.
- Keep logs structured and never log credentials, kubeconfigs, or raw authorization tokens.

## Generated Artifacts
- Run `task mockery` after interface changes that affect mocks.
- Review coverage and generated changes separately from manual logic changes when possible.
- Do not hand-edit generated mocks.

## Do Not
- Hand-edit generated mocks; run `task mockery` after interface changes.
- Treat kcp discovery or cache changes as low-risk; verify them with focused tests.
- Update `.testcoverage.yml` unless the task explicitly requires it.

## Hard Boundaries
- Do not invent new local workflows when a `task` target already exists.
- Treat authorization decision changes as high-risk and verify with focused tests.
- Ask before changing release flow, CI wiring, published images, or Helm integration outside this repo.

## Human-Facing Guidance
- Use `CONTRIBUTING.md` for testing, mockery, and telepresence-based debugging guidance.
- Use `README.md` for kcp configuration expectations and deployment context.
