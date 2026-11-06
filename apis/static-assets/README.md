# static-assets

Central home for the kcp and Kubernetes manifests the platform-mesh components
ship. Every directory exposes a `Resources embed.FS` through an `embed.go`, so
components and their tests embed from here instead of keeping copies.

- `kcp/`: the manifest tree platform-mesh-operator applies to kcp at setup
  (workspaces, APIExports, APIResourceSchemas, bindings, content configurations).
  Files in `kcp/` target `root`; each `NN-<name>/` subdirectory targets the
  workspace `<name>` and is applied in `NN` order.
- `features/<feature>/`: the kcp manifests applied when that feature toggle is
  enabled, laid out like `kcp/`.
- `k8s/`: Kubernetes manifests the operator applies to the runtime cluster.
- `resources/<component>/`: apigen output (APIExport + APIResourceSchemas) of
  each component, written by `task generate` in that component. The deployed
  copies under `kcp/` are curated by hand from these.

Schema names are `v<yymmdd>-<hash>` where `<hash>` is derived from the schema
content (see `hack/apigen.sh`), so an unchanged API keeps its name.
