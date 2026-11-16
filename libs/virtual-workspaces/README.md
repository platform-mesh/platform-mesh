# virtual-workspaces

Go packages for building a [kcp](https://github.com/kcp-dev/kcp) virtual workspace API server. It holds the code every Platform Mesh virtual workspace needs and none of the workspaces themselves.

| Package | Purpose |
|---------|---------|
| `server` | Flags and bootstrap: `server.Run` starts the API server with the workspaces a callback returns. |
| `authentication` | Bearer token authenticator that validates tokens against kcp for the resolved cluster path. |
| `authorization` | Authorization attributes helper. |
| `path` | Root path resolver for `/services/<name>/clusters/<path>` URLs. |
| `proxy` | Resolves and proxies requests to the upstream kcp. |
| `storage` | Storage helpers for dynamic, filtered resources. |
| `apidefinition` | API definition set serving a single resource. |

## Users

- [services/marketplace](../../services/marketplace): the `marketplace` virtual workspace.
- [operators/extension-manager-operator/virtual-workspace](../../operators/extension-manager-operator/virtual-workspace): the `contentconfigurations` virtual workspace.

## kcp fork of Kubernetes

The kcp virtual workspace framework builds against the kcp fork of Kubernetes, which [`../go.mod`](../go.mod) selects with a `replace` directive for every `k8s.io` module. Go ignores the `replace` directives of a dependency, so each module importing these packages has to carry the same block; keep the three in step when the kcp version changes.

## Development

```sh
task lint-libs
task test-libs
```
