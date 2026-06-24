> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# kubernetes-graphql-gateway

Expose Kubernetes resources as a GraphQL API. This enables UIs and tools to query, mutate, and subscribe to cluster resources in a developer-friendly way using the GraphQL ecosystem.

## Architecture

The gateway consists of two components:

```
┌──────────────────┐      ┌─────────────┐  gRPC / files  ┌──────────────────┐
│                  │      │             │───────────────▶│                  │
│  Kubernetes API  │─────▶│  Listener   │                │    Gateway       │─────▶ GraphQL API
│  Server(s)       │      │             │                │                  │      /api/clusters/{name}/graphql
│                  │      │  Extracts   │                │  Serves GraphQL  │
└──────────────────┘      │  OpenAPI →  │                │  queries,        │
                          │  GraphQL    │                │  mutations,      │
                          │  schemas    │                │  subscriptions   │
                          └─────────────┘                └──────────────────┘
```

- **Listener**: Connects to Kubernetes clusters, extracts their OpenAPI v3 specs, and converts them into GraphQL schemas.
- **Gateway**: Receives schemas from the listener, builds per-cluster GraphQL endpoints, and serves them over HTTP with authentication, query validation, and real-time subscriptions.

### Schema Transport

The listener and gateway communicate via `--schema-handler`, which both components must agree on:

| Mode | Description |
|---|---|
| `grpc` | The listener runs a gRPC server and the gateway connects as a client. Schemas are streamed in real-time. This is the recommended mode. |
| `file` | The listener writes schema JSON files to a shared directory (`--schemas-dir`). The gateway watches the directory with fsnotify. Useful for debugging or when the two components cannot connect directly. |

## Quick Start

### Prerequisites

- Go 1.26+
- A running Kubernetes cluster with a valid kubeconfig (e.g. kind, minikube)
- [Task](https://taskfile.dev) (`go install github.com/go-task/task/v3/cmd/task@latest`)

### Run locally (single cluster)

**Terminal 1** — Start the listener:

```sh
go run main.go listener --schema-handler grpc
```

This starts the listener in `single` mode with a gRPC server on `:50051`. It watches namespaces on your local cluster and when it finds the `default` namespace (the anchor resource), generates and streams the GraphQL schema to connected gateways.

**Terminal 2** — Start the gateway:

```sh
go run main.go gateway --schema-handler grpc --enable-playground
```

This starts the gateway on port `8080` with the GraphQL playground enabled. It connects to the listener's gRPC server and receives schemas, creating an endpoint at `/api/clusters/single/graphql`.

**Try it** — Open `http://localhost:8080/api/clusters/single/graphql` in your browser and run:

```graphql
{
  namespaces {
    items {
      metadata {
        name
        labels
      }
    }
  }
}
```

### Run with ClusterAccess (remote clusters)

To connect the listener to remote clusters, use the `ClusterAccess` CRD:

1. Install the CRD on the management cluster:

```sh
kubectl apply -f config/crd/gateway.platform-mesh.io_clusteraccesses.yaml
```

2. Create a `ClusterAccess` resource pointing to the target cluster (see [clusteraccess-serviceaccount.yaml](config/examples/clusteraccess-serviceaccount.yaml) for a full example):

```yaml
apiVersion: gateway.platform-mesh.io/v1alpha1
kind: ClusterAccess
metadata:
  name: my-cluster
spec:
  host: https://<cluster-api-server>
  auth:
    serviceAccountRef:
      name: graphql-gateway
      namespace: graphql-gateway
```

3. Start the listener with the ClusterAccess controller enabled:

```sh
go run main.go listener --schema-handler grpc --enable-clusteraccess-controller
```

4. Start the gateway:

```sh
go run main.go gateway --schema-handler grpc --enable-playground
```

5. Query at: `http://localhost:8080/api/clusters/my-cluster/graphql`

The helper script [`hack/create-clusteraccess.sh`](hack/create-clusteraccess.md) can automate ClusterAccess resource creation.

## GraphQL API

For every Kubernetes resource discovered on a cluster, the gateway generates typed GraphQL operations:

### Queries

| Operation | Description | Key Arguments |
|---|---|---|
| `{pluralName}` | List resources | `namespace`, `labelselector`, `limit`, `continue`, `sortBy` |
| `{singularName}` | Get a single resource | `name`, `namespace` |
| `{singularName}Yaml` | Get a single resource as YAML string | `name`, `namespace` |

### Mutations

| Operation | Description | Key Arguments |
|---|---|---|
| `create{Name}` | Create a resource | `namespace`, `object`, `dryRun` |
| `update{Name}` | Patch a resource (merge patch) | `name`, `namespace`, `object`, `dryRun` |
| `delete{Name}` | Delete a resource | `name`, `namespace`, `dryRun` |
| `applyYaml` | Create-or-update from a YAML string | `yaml` |

### Subscriptions

Real-time updates via Server-Sent Events (`Accept: text/event-stream`):

| Operation | Description | Key Arguments |
|---|---|---|
| `{group}_{version}_{singularName}` | Watch a single resource | `name`, `namespace`, `resourceVersion` |
| `{group}_{version}_{pluralName}` | Watch a list of resources | `namespace`, `labelselector`, `subscribeToAll`, `resourceVersion` |

Each event is an envelope with `type` (`ADDED`, `MODIFIED`, `DELETED`) and `object`.

By default, `MODIFIED` events are only sent when the fields you selected in the subscription query actually change. Set `subscribeToAll: true` to receive all modifications.

## Multi-Cluster Modes

The listener supports three provider modes via `--multicluster-runtime-provider`:

| Mode | Flag Value | Description |
|---|---|---|
| **Single** | `single` (default) | Watches the local cluster from your kubeconfig |
| **kcp** | `kcp` | Connects to [kcp](https://kcp.io) workspaces via APIExport virtual workspaces |
| **Multi** | `multi` | Combines kcp + standard clusters. Use `--single-kubeconfig` for the standard cluster and `--kubeconfig` for kcp |

In any mode, enable `--enable-clusteraccess-controller` to additionally manage remote clusters via the `ClusterAccess` CRD.

### ClusterAccess Authentication Methods

The `ClusterAccess` CRD supports four authentication methods (mutually exclusive):

| Method | Field | Description |
|---|---|---|
| Service Account | `auth.serviceAccountRef` | Generates tokens from a service account on the management cluster |
| Bearer Token | `auth.tokenSecretRef` | References a secret containing a bearer token |
| Kubeconfig | `auth.kubeconfigSecretRef` | References a secret containing a full kubeconfig |
| Client Certificate | `auth.clientCertificateRef` | References a TLS secret with `tls.crt` and `tls.key` for mTLS |

Optionally set `ca.secretRef` for custom CA certificates.

## Configuration Reference

### Gateway Flags

| Flag | Default | Description |
|---|---|---|
| `--schemas-dir` | `_output/schemas` | Directory to watch for schema files |
| `--schema-handler` | `file` | How to receive schema updates: `file` or `grpc` |
| `--grpc-listener-address` | `localhost:50051` | gRPC listener address (when `--schema-handler=grpc`) |
| `--grpc-max-recv-msg-size` | `4194304` (4 MB) | Max gRPC receive message size in bytes (when `--schema-handler=grpc`) |
| `--gateway-port` | `8080` | Port for the GraphQL server |
| `--gateway-address` | `0.0.0.0` | Bind address for the GraphQL server |
| `--enable-playground` | `false` | Enable the GraphQL playground UI |
| `--cors-allowed-origins` | (none) | Allowed origins for CORS |
| `--cors-allowed-headers` | (none) | Allowed headers for CORS |
| `--endpoint-suffix` | `/graphql` | Suffix appended to cluster endpoint paths |
| `--token-review-cache-ttl` | `30s` | Cache TTL for Kubernetes TokenReview results |
| `--request-timeout` | `60s` | Max duration for GraphQL requests |
| `--subscription-timeout` | `30m` | Max duration for SSE subscriptions |
| `--max-request-body-bytes` | `3145728` (3 MB) | Max request body size |
| `--max-inflight-requests` | `400` | Max concurrent requests |
| `--max-inflight-subscriptions` | `50` | Max concurrent SSE subscriptions |
| `--max-query-depth` | `10` | Max query nesting depth |
| `--max-query-complexity` | `1000` | Max query complexity score |
| `--max-query-batch-size` | `10` | Max queries per batch request |
| `--read-header-timeout` | `32s` | Max duration for reading request headers |
| `--idle-timeout` | `90s` | Max idle duration for keep-alive connections |

Set any limit flag to `0` to disable that limit.

### Listener Flags

| Flag | Default | Description |
|---|---|---|
| `--kubeconfig` | (auto-detected) | Path to kubeconfig (required if out-of-cluster) |
| `--multicluster-runtime-provider` | `single` | Provider mode: `single`, `kcp`, or `multi` |
| `--schemas-dir` | `_output/schemas` | Directory to store generated schema files |
| `--schema-handler` | `file` | Schema transport: `file` or `grpc` |
| `--grpc-listen-addr` | `:50051` | gRPC server address (when `--schema-handler=grpc`) |
| `--grpc-max-send-msg-size` | `4194304` (4 MB) | Max gRPC send message size in bytes (when `--schema-handler=grpc`) |
| `--reconciler-gvr` | `namespaces.v1` | GroupVersionResource the reconciler watches |
| `--anchor-resource` | `object.metadata.name == 'default'` | CEL expression to match the anchor resource |
| `--enable-clusteraccess-controller` | `false` | Enable the ClusterAccess CRD controller |
| `--single-kubeconfig` | (none) | Kubeconfig for the single provider (only with `multi` mode) |
| `--resource-controller-providers` | `kcp` | Providers for resource controller (only with `multi` mode) |
| `--clusteraccess-controller-providers` | `single` | Providers for ClusterAccess controller (only with `multi` mode) |
| `--enable-http2` | `false` | Enable HTTP/2 for the controller-manager server |
| `--metrics-bind-address` | `0` (disabled) | Bind address for the metrics endpoint |
| `--metrics-secure-serve` | `false` | Serve metrics over HTTPS |

## Development

```sh
# Run all validation (generate CRDs, lint, test)
task validate

# Individual steps
task generate       # Generate CRD manifests and deepcopy methods
task lint           # Run golangci-lint
task test           # Run tests with coverage

# Build container image
task docker

# Build and load into a kind cluster
task docker:kind

# Regenerate protobuf files
task proto
```

## Contributing

Contributions are welcome. Please open an issue or pull request.

## Releasing

The release is performed automatically through a GitHub Actions Workflow.
All the released versions will be available through access to GitHub (as any other Golang Module).

## Security / Disclosure

If you find any bug that may be a security problem, please follow our instructions [in our security policy](https://github.com/platform-mesh/.github/blob/main/SECURITY.md) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>

