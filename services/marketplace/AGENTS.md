## Repository Description
- `marketplace` is the kcp virtual workspace serving `MarketplaceEntry` resources to the marketplace UI.
- This is a Go API server repo built on [kcp](https://github.com/kcp-dev/kcp) and Kubernetes apiserver libraries rather than controller-runtime-style reconciliation.
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. Prefer the simplest change that fixes the real problem.
- Verify the affected path before finishing. Use targeted tests first.
- Prefer the existing `task` targets where available; otherwise use direct Go commands.
- Keep human-facing process details in `CONTRIBUTING.md`; keep this file focused on agent execution.

## Project Structure
- `cmd`: CLI entrypoint; `cmd/start.go` hands the marketplace workspace to the server.
- `pkg/marketplace`: the virtual workspace (`server.go`) and the storage filter computing the entries (`filter.go`).
- `golang-commons/virtual-workspaces` (repo root, module `go.platform-mesh.io/golang-commons/virtual-workspaces`): the shared virtual workspace API server — `server` (flags, bootstrap), `authentication`, `authorization`, `proxy`, `storage`, `path`, `apidefinition`.
- `pkg/config`: flags of the marketplace workspace.
- `config/crd`, `apis/static-assets/resources/marketplace`: generated resource output.

## Architecture
This is a kcp virtual workspace server serving one resource, `MarketplaceEntry`.

- `pkg/marketplace/server.go` bootstraps a dynamic virtual workspace from the embedded, generated APIResourceSchema.
- `pkg/marketplace/filter.go` replaces the list: entries are computed per request from ProviderMetadata, APIExports and the APIBindings of the requested workspace.
- The custom authenticator validates bearer tokens by probing the upstream kcp `/version` endpoint for the resolved cluster path; authorization is intentionally lightweight.
- The server packages live in `golang-commons/virtual-workspaces` and are shared with `operators/extension-manager-operator/virtual-workspace` (the contentconfigurations virtual workspace); run both modules' tests after changing them.

## Commands
- `go test ./...` — default test command.
- `go test ./... -run <name>` — narrow verification for a specific path.
- `go fmt ./...` — format Go code.
- `task manifests` — regenerate CRDs.
- `task generate` — regenerate API objects and resource output after API changes.
- `go run ./main.go start` — run the virtual workspace server locally.
- `task docker-build` — build the container image.
- `task docker:kind` — build, load, and restart the deployment in kind.

## Code Conventions
- Follow existing package boundaries before adding new abstractions.
- Keep API-server wiring in `cmd` and request-path-specific logic in the corresponding `pkg/*` package.
- Add or update `_test.go` files when changing behavior.
- When editing API types, regenerate derived output instead of hand-editing generated artifacts.
- Treat authentication, authorization, and proxying changes as high-risk; validate carefully.
- Keep logs structured and avoid logging credentials, tokens, or raw auth material.

## Generated Artifacts
- Run `task generate` after changing API types or CRD shape.
- Review changes in `config/crd` and `apis/static-assets/resources/marketplace`.
- Do not mix unrelated manual edits into generated files.

## Do Not
- Edit generated CRD or resource output in `config/crd` or `apis/static-assets/resources/marketplace` without running `task generate`.
- Treat broad authn/authz or proxy changes as routine refactors; verify them explicitly.

## Hard Boundaries
- Do not invent task targets or repo conventions that do not exist here.
- Ask before changing release flow, CI wiring, deployment shape, or Helm integration.
- Be careful with broad authn/authz changes; prefer the narrowest possible fix and explicit verification.

## Human-Facing Guidance
- Use `README.md` for local certificate setup, startup arguments, and service context.
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.
