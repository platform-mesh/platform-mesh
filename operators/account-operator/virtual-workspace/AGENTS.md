## Repository Description
- `access-virtual-workspaces` is the kcp virtual workspace answering `SelfClusterAccessReview`: which workspaces a caller can access, from OpenFGA (`--provider=openfga`) or from kcp RBAC (`--provider=rbac`).
- It lives inside the account-operator directory because it answers for the tenancy model (accounts) that operator owns. It is a separate Go module (`go.platform-mesh.io/account-operator/virtual-workspace`): it builds against the kcp fork of Kubernetes and shares no code with the operator.
- It is a Go API server built on the kcp virtual-workspace-framework and Kubernetes apiserver libraries, not a controller-runtime reconciler.
- Read the org-wide [AGENTS.md](https://github.com/platform-mesh/.github/blob/main/AGENTS.md) for general conventions.

## Core Principles
- Keep changes small and local. Prefer the simplest change that fixes the real problem.
- Verify the affected path before finishing. Use targeted tests first.
- The code was imported from kcp-dev/contrib-access-virtual-workspace: imported files keep the kcp copyright header (see `hack/boilerplate/skip.txt`).

## Project Structure
- `cmd/access-vw`: the server; `cmd/init`: bootstrap for the `rbac` provider; `cmd/scar-to-kubeconfig`: client.
- `pkg/server`: flags, authentication, provider selection (`server.go`, `options.go`).
- `pkg/virtual/scar`: the virtual workspace, its URL handling (`/services/access[/clusters/<cluster>]`) and the review storage.
- `pkg/accessprovider`: the seams, named as in ADR 011: `AccessProvider` answers a review for an `Identity` (name, groups, extra), `GraphPopulator` feeds the graph. `GraphAdapter` serves the graph as an `AccessProvider`, `Union` merges several (`union.go`).
- `pkg/openfgaprovider`: the OpenFGA resolver and its kcp lookups (`resolver.go`, `directory.go`).
- `pkg/rbacprovider`, `pkg/graph`: the RBAC indexer and its in-memory graph.
- `sdk/apis/access/v1alpha1`: API types, group `access.platform-mesh.io`; deepcopy and `pkg/generated/openapi` are checked in.
- `config/apiexport`, `config/controller`: manifests embedded by `cmd/init`.

## Architecture
- A review is answered by an `accessprovider.AccessProvider`. `GraphAdapter` wraps the RBAC graph; `openfgaprovider.Resolver` asks OpenFGA per review and keeps no state.
- `--provider=rbac,openfga` runs both behind `accessprovider.Union`: results are merged and deduplicated by cluster, and the union fails closed (one provider failing or not ready fails the review). Because the OpenFGA resolver rejects unscoped requests, every review has to be scoped then.
- The OpenFGA resolver needs the workspace a review is addressed to: it reduces it to its organization, looks up the organization's store and makes one `ListObjects` call. It must fail with 503 rather than return a partial answer.
- The OpenFGA resolver reads kcp through the virtual workspaces of the APIExports `core.platform-mesh.io` and `openfga.platform-mesh.io` (`NewExportDirectory`, `--openfga-*-endpointslice`), with the read-only scoped kubeconfig platform-mesh-operator mints (`access-vw-kubeconfig` in `DefaultProviderConnections`). The server must not need an administrator of kcp; reading workspaces directly (`NewKCPDirectory`) is the fallback without the exports.
- The RBAC graph is keyed on bare subject names; `GraphAdapter` narrows an answer to the clusters the identity is valid in (service account cluster, `authentication.kcp.io/scopes`).
- Authentication is the front-proxy's; the server reads the caller from the request headers.

## Commands
- `go test ./...` — default test command.
- `go build ./...` — build all three binaries' packages.
- `task lint` — format and lint with the repository settings.
- `task docker-build` — build the container image (context is the repository root).

## Code Conventions
- Follow the existing package boundaries before adding new abstractions.
- Access providers return API status errors; the storage passes them to the caller unchanged.
- Add or update `_test.go` files when changing behavior.
- The `replace` block of `go.mod` pins kcp's fork of Kubernetes and has to match `github.com/kcp-dev/virtual-workspace-framework`.
