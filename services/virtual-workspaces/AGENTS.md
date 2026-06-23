## Repository Description
- `virtual-workspaces` provides custom kcp-backed virtual workspace APIs for Platform Mesh.
- It exposes API-server-style endpoints for Platform Mesh views such as content configuration and marketplace access.
- This is a Go API server repo built on [kcp](https://github.com/kcp-dev/kcp) and Kubernetes apiserver libraries rather than controller-runtime-style reconciliation.
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. Prefer the simplest change that fixes the real problem.
- Verify the affected path before finishing. Use targeted tests first.
- Prefer the existing `task` targets where available; otherwise use direct Go commands.
- Keep human-facing process details in `CONTRIBUTING.md`; keep this file focused on agent execution.

## Project Structure
- `api/v1alpha1`: API types for exposed resources.
- `cmd`: CLI entrypoints such as service startup.
- `pkg/apidefinition`: API registration and definition helpers.
- `pkg/authentication`, `pkg/authorization`: authn/authz behavior.
- `pkg/contentconfiguration`, `pkg/marketplace`: domain handlers for exposed resources.
- `pkg/proxy`, `pkg/storage`, `pkg/path`: request routing and backend access helpers.
- `pkg/config`: runtime configuration handling.
- `config/crd`, `config/resources`: generated resource output.

## Architecture
This repo is a kcp virtual workspace server. Most changes affect request routing, authn/authz, or storage-backed API behavior.

### Runtime model
- `cmd/start.go` builds a `virtualrootapiserver` and registers two named virtual workspaces: `contentconfigurations` and `marketplace`.
- Authentication is layered: the default delegating authenticator is wrapped with a custom bearer-token authenticator from `pkg/authentication`.
- Authorization is set with kcp's virtual workspace authorizer plus per-workspace attribute handling.

### Virtual workspace model
- `pkg/contentconfiguration/server.go` bootstraps a dynamic virtual workspace from an APIResourceSchema fetched from kcp and proxies requests into storage-backed content configuration data.
- `pkg/marketplace/server.go` bootstraps a second dynamic virtual workspace from an embedded/generated resource schema and marketplace-specific storage filtering.
- Both workspaces rely on cluster-path resolution and proxy/storage helpers; path handling bugs can affect every request.

### Authentication and authorization
- The custom authenticator validates bearer tokens by probing the upstream kcp `/version` endpoint for the resolved cluster path.
- Authentication success currently maps users into `system:authenticated`; authorization logic is intentionally lightweight and largely path/attribute based.

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
- Review changes in `config/crd` and `config/resources`.
- Do not mix unrelated manual edits into generated files.

## Do Not
- Edit generated CRD or resource output in `config/crd` or `config/resources` without running `task generate`.
- Treat broad authn/authz or proxy changes as routine refactors; verify them explicitly.

## Hard Boundaries
- Do not invent task targets or repo conventions that do not exist here.
- Ask before changing release flow, CI wiring, deployment shape, or Helm integration outside this repo.
- Be careful with broad authn/authz changes; prefer the narrowest possible fix and explicit verification.

## Human-Facing Guidance
- Use `README.md` for local certificate setup, startup arguments, and service context.
- Use `CONTRIBUTING.md` for contribution process, DCO, and broader developer workflow expectations.

