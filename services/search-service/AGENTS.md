## Repository Description
- `search` provides the Platform Mesh search HTTP service.
- The core request flow is kcp-backed tenant/index resolution, OpenSearch querying, and OpenFGA-based authorization filtering before results are returned to the caller.
- This is a Go service built around [chi](https://github.com/go-chi/chi), [OpenSearch Go](https://github.com/opensearch-project/opensearch-go), and [OpenFGA](https://github.com/openfga/api).
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes narrow. Search correctness depends on the interaction between routing, kcp lookup, OpenSearch queries, and authz filtering.
- Preserve request validation and pagination behavior unless the task explicitly requires changing API semantics.
- Verify behavior before finishing. Start with package-level tests and compile checks.
- Keep this file focused on agent execution and repository-specific constraints.

## Project Structure
- `cmd`: CLI wiring and service startup.
- `internal/router`: HTTP routes and request-to-service translation.
- `internal/service/search`: search orchestration, pagination, cursor handling, and result mapping.
- `internal/clients/kcp`: kcp clients for org access validation and `SearchIndex` resolution.
- `internal/clients/opensearch`: OpenSearch client integration.
- `internal/clients/fga`: OpenFGA authorization filtering.
- `internal/middleware`, `internal/context`, `internal/observability`: request context and service instrumentation.

## Architecture
This is an HTTP service, not an operator.

### Request flow
- `cmd/server.go` wires the service dependencies, including kcp access validation, `SearchIndex` resolution, OpenSearch, OpenFGA, middleware, and the HTTP server.
- `internal/router/router.go` exposes `/rest/v1/search` plus health endpoints and translates query params plus request context into `SearchRequest`.
- `internal/service/search/service.go` resolves the tenant's search index, queries OpenSearch in batches, filters hits through OpenFGA, and emits cursor-based pagination.

### Domain model
- The service is scoped by organization and user context supplied through middleware.
- `SearchIndex` tells the service which OpenSearch index to query for a given organization.
- Returned hits include both searchable metadata and the original source payload used by downstream consumers.

## Commands
- `go test ./...` — run the local test suite.
- `go build ./...` — compile all packages.
- `go run ./main.go serve` — start the service locally.
- `go fmt ./...` — format Go code.

## Code Conventions
- Keep transport logic in `internal/router` and business logic in `internal/service/search`.
- Preserve validation, cursor encoding, and authz filtering invariants when changing the search flow.
- Add or update `_test.go` files alongside behavior changes.
- Keep logs and errors structured, and avoid exposing secrets or unnecessary backend detail.

## Generated Artifacts
- This repo appears light on generated assets; prefer ordinary source edits unless generation is explicitly introduced.

## Do Not
- Bypass authorization filtering when changing search behavior.
- Change cursor or pagination semantics casually.
- Fold kcp, OpenSearch, and OpenFGA concerns together in a way that blurs existing boundaries.

## Hard Boundaries
- Ask before changing the public HTTP contract in a backward-incompatible way.
- Be careful with default limits and backend query fan-out; these affect runtime safety as well as correctness.

## Human-Facing Guidance
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.
