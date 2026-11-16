> [!WARNING]
> This service is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# Platform Mesh marketplace

A kcp virtual workspace that serves `MarketplaceEntry` resources (`marketplace.platform-mesh.io`) to the marketplace UI, behind the kcp front-proxy at `/services/marketplace`.

A list in a workspace returns one entry per APIExport of every provider: the provider's `ProviderMetadata`, the `APIExport`, and the name of the `APIBinding` in that workspace if the export is bound there. Nothing is stored; entries are computed per request.

`pkg/marketplace` holds the workspace; the virtual workspace API server around it (bootstrap, authentication, authorization, path resolution, storage) comes from the shared library [libs/virtual-workspaces](../../libs/virtual-workspaces). The module builds against the kcp fork of Kubernetes, which `go.mod` selects with `replace` directives for every `k8s.io` module.

## Running locally

Generate a serving certificate (with `mkcert` installed and its CA trusted):

```bash
mkcert -cert-file=.secret/apiserver.crt -key-file=.secret/apiserver.key localhost
```

```bash
go run ./main.go start \
  --tls-cert-file=.secret/apiserver.crt \
  --tls-private-key-file=.secret/apiserver.key \
  --secure-port=6443 \
  --bind-address=0.0.0.0 \
  --kubeconfig=.secret/kubeconfig.yaml \
  --authentication-kubeconfig=.secret/authentication.yaml \
  --client-ca-file=.secret/kcp-ca.crt \
  --authentication-skip-lookup \
  --resource-apiexport-endpointslice-name=ui.platform-mesh.io
```

`--kubeconfig` points at the workspace holding the `ui.platform-mesh.io` APIExport; `--server-url` replaces its server with the root of the kcp instance (the URL only, not a cluster) for cluster-aware requests.

## Development

```bash
task test       # unit tests
task lint
task generate   # MarketplaceEntry CRD and APIResourceSchema, after changing apis/marketplace
```

The image is built from the repository root: `task images-marketplace`.
