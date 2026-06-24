> [!WARNING]
> This repository is under development and not ready for productive use. It is in an alpha stage. APIs and concepts may change on short notice, including breaking changes.

# Platform Mesh - search-service

## Description

The platform-mesh `search-service` provides a REST API to query resources indexed in OpenSearch and post-filter results through OpenFGA authorization checks.

The service is organization-aware and derives org context from the request host. It resolves the active SearchIndex in kcp (`root:orgs`) and uses `status.indexName` as source of truth for the OpenSearch index.

## Features

- REST endpoints:
  - `GET /rest/v1/search`
  - `GET /rest/v1/search/resources`
  - `GET /rest/v1/search/filter-values`
- Free-text search in OpenSearch with stable cursor pagination (`search_after`)
- OpenFGA post-filtering (`relation=get`) with fail-closed behavior for incomplete auth context
- Org-aware context + kcp token/org access pre-check
- SearchIndex-driven resource/field metadata:
  - `defaultFields` drive searchable fields
  - `filterableFields` drive exact-match filters
  - `semanticFields` are exposed as metadata (no semantic query mode yet)
- Health endpoints: `/healthz`, `/readyz`

## API

### Search endpoint

`GET /rest/v1/search?q=<query>&limit=<n>&cursor=<opaque>&resource=<plural>&filter.<field>=<value>`

Query params:

- `q` (required): free-text query
- `resource` (optional): plural resource name; if omitted, searches across all resources
- `filter.<field>` (optional, repeatable): exact-match filters; requires `resource`
- `limit` (optional): default `20`, max `100`
- `cursor` (optional): opaque pagination cursor

Response shape:

- `results[]` with compact fields (`id`, `score`, `kind`, `name`, `namespace`, `apiGroup`, `apiVersion`, `workspacePath`, `clusterName`, `organizationId`, `organizationName`, `accountId`, `accountName`)
- `results[].resource` indicates which resource index produced the hit
- `source` containing the raw indexed document source per hit
- `nextCursor` for pagination

### Resource metadata endpoint

`GET /rest/v1/search/resources`

Returns all searchable resources for the org with:

- `resource`
- `defaultFields`
- `filterableFields`
- `semanticFields`

### Filter values endpoint

`GET /rest/v1/search/filter-values?resource=<plural>&field=<filterable>&q=<optional>&filter.<field>=<value>`

Returns distinct authorized values for one filterable field within a single resource.

## Getting Started

### Requirements

- Go `1.25+` (see [go.mod](go.mod))
- Access to:
  - kcp API (for org access check + SearchIndex resolution)
  - OpenSearch
  - OpenFGA gRPC endpoint

### Run locally

Example:

```bash
export OPENSEARCH_USERNAME=<username>
export OPENSEARCH_PASSWORD=<password>

go run . serve \
  --kubeconfig ~/.kube/config \
  --is-local=true \
  --opensearch-url http://localhost:9200 \
  --openfga-grpc-addr localhost:8081
```

### Local Development Mode (`--is-local=true`)

Use `--is-local=true` for local development to match the local behavior of `kubernetes-graphql-gateway`.

When enabled:

- org context is still derived from host (`localhost` is mapped to `--local-development-org`)
- JWT claims are still parsed for user/tenant context
- kcp org token validation (`ValidateTokenForOrg`) is bypassed

This is intended for local/dev usage only. Keep `--is-local=false` for production-like environments.

### Configuration flags

Main runtime flags (with defaults):

- `--port` (default: `8080`)
- `--opensearch-url` (default: `http://opensearch.platform-mesh-system.svc.cluster.local:9200`)
- `--opensearch-username` (default: value of env `OPENSEARCH_USERNAME`)
- `--opensearch-password` (default: value of env `OPENSEARCH_PASSWORD`)
- `--opensearch-insecure` (default: `false`)
- `--opensearch-timeout` (default: `10s`)
- `--openfga-grpc-addr` (default: `openfga:8081`)
- `--searchindex-workspace-path` (default: `root:orgs`)
- `--searchindex-group` (default: `core.platform-mesh.io`)
- `--searchindex-version` (default: `v1alpha1`)
- `--searchindex-resource` (default: `searchindexes`)
- `--search-default-limit` (default: `20`)
- `--search-max-limit` (default: `100`)
- `--search-fetch-batch-size` (default: `100`)
- `--search-max-scanned-hits` (default: `1000`)
- `--is-local` (default: `false`) enables local development behavior in auth middleware
- `--local-development-org` (default: env `SEARCH_LOCAL_ORG`, fallback `local`) org used when host is `localhost`

Global flags from `golang-commons` are also available (e.g. logging and kubeconfig related flags).

### Run tests

```bash
go test ./...
```

## Security Notes

- JWT signature validation is expected to happen upstream (gateway/mesh).
- The service consumes parsed claims from context (`mail`, fallback `sub`).
- Search hits missing required authorization hierarchy fields are dropped (fail-closed).

## Releasing

Releases are performed via GitHub Actions workflows.

## Contributing

Contributions are welcome via pull requests in the Platform Mesh GitHub organization.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for expected conduct when contributing.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
