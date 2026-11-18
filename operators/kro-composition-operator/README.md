# kro-composition-operator

A kcp-native **composition-as-a-service** operator. Consumers author
[kro](https://kro.run) `ResourceGraphDefinition`s (RGDs) in their own Account
(workspace); the operator publishes the generated composite type as a first-class
bound API in that workspace and materializes each instance's child resources —
composing *other* Platform Mesh provider APIs. No workloads are run: kro composes
control-plane objects, and the composed providers do the actual work.

## How it works

- **Watch (up):** one multicluster-runtime manager watches RGDs across *all*
  workspaces that installed KROaaS, via the `kro.run` **APIExport** virtual
  workspace.
- **Publish (per workspace):** for each RGD the operator makes the generated
  composite type a **bound API** in the consumer workspace — it snapshots the CRD
  into an `APIResourceSchema`, exposes it through a per-RGD `APIExport`, and
  self-binds that export (empty export path → the local logical cluster). A plain
  reflected CRD is invisible to the platform's portal machinery: the
  graphql-gateway schema and the pm-openfga-operator's OpenFGA authorization model are
  both generated from APIBinding `boundResources`, not from workspace CRDs. Being a
  boundResource is what makes the composite type first-class (portal list/detail +
  authz) with no extra wiring.
- **Write (children):** the operator materializes each instance's children
  **directly** into the consumer workspace as its own kcp identity. Deployed as a
  ManagedProvider with an `adminAuth: true` connection, that identity is
  cluster-admin, so it can write into any consumer workspace — nothing is
  provisioned per Account.
- **Isolation:** the composite type is published + bound per workspace, so two
  Accounts can author the same type name with different schemas without collision.

The kro engine (`pkg/graph`, `pkg/runtime`, `pkg/dynamiccontroller`) is embedded as
a library; kro is not run as a separate controller.

Supported RGD node types: resources, `forEach` collections, external refs, and
external collections (by selector). Teardown on RGD delete is **finalizer-driven and
ordered**: the operator deletes the APIBinding first and waits for it to be fully
removed — so the platform's binding finalizer strips the type's OpenFGA authz —
before removing the APIExport + APIResourceSchema (which are also owned by the RGD as
a GC backstop). Deleting an instance garbage-collects its children via owner refs.

## Why a custom operator (vs. stock kro / krop)

Stock [kro](https://kro.run) is single-cluster and not kcp-aware: it does **not**
support multicluster-runtime (MCR), and there are no plans upstream to add it at the
moment. Running kro's composition engine across kcp workspaces — watching RGDs
through the `kro.run` APIExport virtual workspace and acting per workspace — requires
MCR. So KROaaS embeds kro's engine (`pkg/graph`, `pkg/runtime`,
`pkg/dynamiccontroller`) as a **library** and drives it with its own
multicluster-runtime manager, instead of running kro as-is.

[krop](https://github.com/opendefensecloud/krop-controller) is a related alpha
project that also embeds kro's engine and runs an MCR manager, so it closes the same
MCR gap. The real difference is **who defines the composite types, and where**:

- **krop is top-down** — the platform provider defines the composite types centrally
  and publishes them for tenants to use.
- **KROaaS is self-service** — each tenant (Account) writes their own RGDs in their
  own workspace, and the operator turns each one into a first-class API right there.

In practice that gives tenants self-service (add or change composite types without
involving the provider) and isolation (each tenant's types stay confined to their own
workspace).

## Build & test

```sh
task build          # go build ./...
task test           # unit tests
task lint
task docker-build   # image (build context is the repo root; -f Dockerfile ../../)
task kind-load
```

## Deploy

`config/` is a kustomize base (ServiceAccount + leader-election RBAC + Deployment):

```sh
kubectl apply -k config/default
```

The Deployment mounts a **`kcp-kubeconfig`** Secret (the operator's kcp identity,
from provider bootstrap) and passes `--kubeconfig=/kubeconfig/kubeconfig`. Flags:

| Flag | Default | Purpose |
|------|---------|---------|
| `--provider-workspace` | `root:providers:kro-provider` | workspace holding the `kro.run` APIExport + endpointslice |
| `--apiexport-endpointslice` | `kro.run` | APIExportEndpointSlice to serve |
| `--leader-elect` | `false` | leader election |

> The Helm chart lives in [`platform-mesh/helm-charts`](https://github.com/platform-mesh/helm-charts)
> at `charts/kro-composition-operator`, per the monorepo convention — not in this
> directory. It is wired into local-setup the httpbin way via the
> `example-kro-provider` kustomize component (gated behind `--example-data`).

## Install flow

**Provider side (once):** in the provider workspace `root:providers:kro-provider`,
publish the `kro.run` APIExport for kro's `ResourceGraphDefinition` type (snapshot
kro's RGD CRD into an APIResourceSchema, then create the APIExport), and an
`APIExportPolicy` in `root:orgs` so Accounts can bind it. These bootstrap manifests
— plus the `ProviderMetadata` marketplace listing and an RGD-list
`ContentConfiguration` — live in `config/provider/`. The operator reads the
APIExportEndpointSlice and watches RGDs via the VW; it writes into consumer
workspaces with its own (adminAuth) kcp identity.

**Consumer side ("install KROaaS" per Account):** bind the `kro.run` APIExport (so
`ResourceGraphDefinition` is served in the workspace) — the Marketplace does this on
order (via the WorkspaceType's default API bindings). Nothing else is provisioned
per Account.

After that a consumer creates RGDs and instances in their workspace and the
operator materializes the composed resources there. An RGD can only compose APIs
already available in that workspace (bound via APIBinding or otherwise served) —
KROaaS composes existing provider APIs, it does not provision them.

## Notes

- The Helm chart + local-setup/OCM wiring live in `platform-mesh/helm-charts`, not
  here (see Deploy). The provider-side bootstrap manifests are in `config/provider/`;
  a per-generated-type `ContentConfiguration` is emitted by the operator at runtime.
