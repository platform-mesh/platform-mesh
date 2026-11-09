## Repository Description
- `golang-commons` provides shared Go packages reused across Platform Mesh services, operators, and controllers.
- The main exported areas are common config, context propagation, logging, middleware, JWT handling, controller/lifecycle helpers, tracing, Sentry integration, and OpenFGA-related helpers.
- This is a Go library repo built around [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime), [multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime), [OpenTelemetry](https://github.com/open-telemetry/opentelemetry-go), and common GraphQL / auth integrations.
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. A breaking change here can cascade across many repositories.
- Prefer extending existing package patterns over introducing new utility layers.
- Verify behavior before finishing. Start with package-level tests, then broader validation.
- Keep this file focused on agent execution; use `README.md` and package READMEs for user-facing context.

## Project Structure
- `config`: shared service/config structs and flag wiring.
- `context`: request/service context helpers, JWT/token/spiffe helpers, and context keys.
- `controller`: reusable controller/lifecycle helpers for operators, including conditions, filtering, and lifecycle orchestration.
- `errors`: shared error helpers and operator error types.
- `fga`: OpenFGA interfaces, store helpers, policy helpers, and supporting utilities.
- `jwt`: token parsing and claim helpers.
- `logger`: zerolog-based logging helpers and test loggers.
- `middleware`: HTTP middleware for tracing, auth, logging, request ids, and sentry recovery.
- `sentry`, `traces`: shared integrations and infrastructure helpers.
- `test`: shared test utilities.

## Architecture
This repo is not a single application. It is a dependency repo, so the most important thing is preserving stable behavior and intended layering between packages.

### Library model
- Packages here are imported by multiple services and operators; changes often have cross-repo consequences even when local tests pass.
- `config`, `context`, `logger`, `middleware`, `traces`, and `sentry` form a common runtime foundation used by service entrypoints.
- `controller` provides reusable lifecycle/controller patterns; see `controller/README.md` for the lifecycle/subroutine model used by several operators.

### Context and middleware model
- `context` is the shared place for request-scoped values such as tenant, auth header, web token, SPIFFE identity, and user id.
- `middleware.CreateMiddleware(...)` is the main composition point for logging, tracing, sentry recovery, request ids, and optional auth extraction.
- Be careful when changing context keys or middleware order; many downstream services depend on the exact propagation behavior.

### Controller and operator support
- The `controller` packages provide common lifecycle, condition, filter, and error behavior reused by Platform Mesh operators.
- Changes in controller helper packages can silently alter reconciliation flow, status handling, or debug-label filtering across multiple repos.

### Generated artifacts and tests
- Mocks are generated through `mockery` using `.mockery.yaml`.
- Coverage thresholds are enforced by `.testcoverage.yml`.
- This repo is test-heavy; most packages already have focused unit tests, and those are the first safety net for changes.

## Commands
- `task fmt` — format Go code.
- `task lint` — run formatting plus golangci-lint; also regenerates mocks first.
- `task mockery` — regenerate mocks from `.mockery.yaml`.
- `task build` — build all packages.
- `task test` — run tests after regenerating mocks.
- `task cover` — run tests with coverage output.
- `task test-coverage` — enforce coverage thresholds from `.testcoverage.yml`.
- `go test ./<pkg>` — fast fallback for targeted package verification.

## Code Conventions
- Keep package APIs stable unless the change explicitly coordinates downstream consumers.
- Prefer package-local additions over cross-cutting helpers that blur existing boundaries.
- Add or update `_test.go` files together with behavior changes.
- When changing shared context, middleware, or controller helpers, think through downstream usage, not just local compile success.
- Keep logs structured and avoid logging raw tokens, secrets, or other sensitive material.

## Generated Artifacts
- Run `task mockery` after interface changes that affect generated mocks.
- Review generated mock updates separately from manual logic changes when possible.
- Use `task test-coverage` when touching behavior that could impact coverage thresholds.

## Do Not
- Hand-edit generated mocks; regenerate them through `task mockery`.
- Update `.testcoverage.yml` unless the task explicitly requires it.
- Treat shared context keys, middleware ordering, or controller lifecycle behavior as isolated local changes.

## Hard Boundaries
- Do not invent new local workflows when a `task` target already exists.
- Ask before making broad API changes that are likely to require synchronized updates in downstream repos.
- Be careful with exported types and function signatures; this repo is consumed widely across the monorepo.

## Human-Facing Guidance
- Use `README.md` and package-level READMEs, especially [`controller/README.md`](controller/README.md), for deeper conceptual background.
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.
