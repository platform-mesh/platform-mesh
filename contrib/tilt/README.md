# Platform Mesh — Tilt local development environment

Tilt replaces the OCM/Flux/platform-mesh-operator delivery pipeline for the
developer inner loop: infrastructure is deployed once into kind, and the
operators/services you are working on hot-reload in seconds.

**kcp is built, not installed.** The base environment runs
`platform-mesh-deployer` and hands it a `PlatformMesh`; the deployer compiles
that into kcp-operator admin CRs, reconciles them itself, and kcp appears. Every
profile sits on the kcp that comes out.

## What it deploys

| | |
|---|---|
| **Local infra** (`manifests/`) | `platform-mesh-system` namespace, a self-signed cert-manager `Issuer`, and Dex |
| **Remote infra** (`infra_remote.Tiltfile`, skipped by `TILT_NO_INFRA=1`) | cert-manager, the envoy gateway controller, Flux, the OCM controller |
| **kcp** (always) | `platform-mesh-deployer` hot-reloaded from source, the kcp-operator CRDs it reconciles, a mutual-TLS etcd, the `eg` gateway, and a dev `PlatformMesh`. See [How kcp gets built](#how-kcp-gets-built) |

## Prerequisites

docker or podman, `kind`, `kubectl`, `helm`, `tilt`, `git`.

Nothing is fetched from the kcp repo: kcp is built in-cluster from a
`PlatformMesh`, so there is no external module to pin or keep in step.

## Usage

```sh
# create the kind cluster first — it MUST be named platform-mesh.
# The Tiltfile refuses any context other than kind-platform-mesh (guard against
# a stray `tilt up` hitting a shared/other cluster). Override with
# TILT_ALLOWED_CONTEXT if you must.
kind create cluster --name platform-mesh
tilt up -f contrib/tilt/Tiltfile

# with a feature layer on top
tilt up -f contrib/tilt/Tiltfile -- --profile=auth

# charts render live from ./charts; set TILT_CHARTS_FROM_OCI=1 to pull the
# pinned published versions instead
```

### Profiles

Profiles are independent feature layers — each only ever *adds* resources, so
several can be requested at once:

```sh
tilt up -f contrib/tilt/Tiltfile -- --profile=core,auth
tilt up -f contrib/tilt/Tiltfile -- --profile=core --profile=auth   # same thing
tilt up -f contrib/tilt/Tiltfile -- --profile=full                  # every profile
```

| Profile | Adds |
|---|---|
| `infra` | the base environment, kcp included. **Always on** — naming it is allowed but redundant |
| `core` | nothing yet (reserved; lands with Phase 1) |
| `auth` | the ReBAC authorization webhook secret on kcp (L3) |
| `full` | shorthand for all of the above |

An unknown profile fails the Tiltfile rather than being silently ignored, so a typo
does not quietly give you a smaller environment than you asked for.

### Validate manifests without a cluster

```sh
TILT_NO_INFRA=1 tilt alpha tiltfile-result -f contrib/tilt/Tiltfile -- --profile=full
```

Renders every manifest and custom resource with all substitutions applied — the
`PlatformMesh`, its topology templates, Dex's issuer — so you can confirm the
wiring before deploying. Note the `-f`: there is no Tiltfile at the repo root, and
`--` separates Tilt's flags from the Tiltfile's own.

## Addressing

Everything is `<name>.<CLUSTER_ID>.sslip.io` on port **31443**, where `CLUSTER_ID`
is the node's InternalIP with dots replaced by dashes (`192-168-97-5`).

| | |
|---|---|
| kcp front proxy | `fp.<id>.sslip.io:31443` |
| root shard / shards | `root.<id>.sslip.io`, `shards-default.<id>.sslip.io` |
| Dex (OIDC issuer) | `idp.<id>.sslip.io:31443` |
| etcd | `etcd.<id>.sslip.io:31443` |

A virtual workspace gets no hostname of its own: it is reached on a path of the
front proxy, which the `FrontProxyTemplate` this environment renders declares
(`FRONT_PROXY_MAPPINGS` in the Tiltfile). That is the same entrypoint a real
installation gets from an `OCMModule` component's `mapping` — one address, one
CA, in dev and in production.

**One scheme, because a kcp hostname has to resolve to the same address from
three places**: the host, the kcp shards, and anything following an
`APIExportEndpointSlice` URL (dialed verbatim — no kubeconfig can redirect it).
sslip.io resolves `root.192-168-97-5.sslip.io` to `192.168.97.5` for all of them.

Consequences worth knowing:

- **No hostAliases anywhere.** The old `*.pm.localhost` scheme resolved to
  loopback inside a pod, so every in-cluster client needed aliasing at a gateway
  IP that was advertised but not routed — a broken route that read as a broken
  credential. That whole mechanism is gone.
- **No ingress port-forward.** The host reaches the node IP directly, so there is
  no long-running tunnel whose death silently breaks every `kubectl`.
- **It must be the NodePort**, not the gateway's listener port: sslip.io resolves
  to the node, and nothing listens on `node:8443`.
- **CoreDNS is patched** (`deployer-dns`) to answer `*.sslip.io` locally. kind
  forwards to the host resolver, which commonly drops answers pointing at private
  IPs (DNS rebinding protection); without the patch every kcp dial fails with
  `no such host`.
- Override the ID with `DEPLOYER_CLUSTER_ID` if the node IP is not what you want.

Reaching the node IP from the host needs a runtime that routes it — OrbStack and
Linux docker do; Docker Desktop for Mac does not, where you would need a
port-forward to `svc/eg-nodeport`.

## How kcp gets built

`platform-mesh-deployer` compiles a `PlatformMesh` plus a `RootShardTemplate` and
a `ShardTemplate` into `RootShard`/`Shard`/`FrontProxy` admin CRs, then — running
kcp-operator's controller groups in its own manager — into `Compiled*` CRs and
Deployments. The wiring lives
with the component, at
[`operators/platform-mesh-deployer/Tiltfile`](../../operators/platform-mesh-deployer/Tiltfile);
this Tiltfile supplies only what is environment-wide (namespace, cluster ID,
port, the OIDC dict).

Shape mirrors `test/e2e`'s `TestSingleCluster`: one cluster acting as its own
config plane and workload cluster.

| Resource | What |
|---|---|
| `deployer-cert-issuer` | the self-signed `kcp` ClusterIssuer the shard CAs root from |
| `deployer-gateway` | the `eg` Gateway + the pinned NodePort — the environment's only gateway |
| `deployer-dns` | CoreDNS `*.sslip.io` answers |
| `deployer-etcd` | mutual-TLS etcd, reachable through the gateway |
| `kcp-operator-crds` | the `operator.kcp.io` and `deploy.operator.kcp.io` CRDs |
| `deployer-crds` | the `deploy.platform-mesh.io` CRDs |
| `platform-mesh-deployer` | the operator, built from source and live-updated |
| `deployer-engage` | the kubeconfig Secret that engages this cluster as its own workload cluster |
| `platformmesh` | the dev `PlatformMesh` and its two topology templates |
| `deployer-admin` | an admin kubeconfig for the built kcp, written to `.secret/kcp/` |

### There is no kcp-operator Deployment

The deployer imports **ntnn/kcp-operator** and registers both of its controller
groups on its own manager: the config group on the config plane, the workload
group on the engaged clusters. Nothing runs the operator as a separate
Deployment, so only its CRDs are installed.

**Bump the ref in `config/bases/kcp-operator/crds` and the `go.mod` replace
together**, or the deployer reconciles CRs against a schema that is not
installed.

### Two resources that delete more than they look like

Tilt **delete-recreates** a resource's objects on a forced update (the trigger
button). For most resources that is harmless. For two it is not:

- `deployer-crds` — deleting a CRD deletes every CR of that type. Removing
  `platformmeshes` takes the `PlatformMesh`, and the `ownerReferences` the
  deployer stamps on what it generates (`controller: true`) cascade that into
  every RootShard, Shard and FrontProxy and their Deployments.
- `platformmesh` — same cascade, one level down.

They are separate resources from `platform-mesh-deployer` precisely so that
restarting the operator (the routine gesture) cannot trigger either.

If a shard is left `Provisioning` with no Deployment after such an event, the
workload controllers are in exponential backoff on a race that has since
resolved:

```sh
kubectl -n platform-mesh-system rollout restart deploy/platform-mesh-deployer-controller-manager
```

### Poking kcp

`deployer-admin` writes an admin kubeconfig once the front proxy is up:

```sh
export KUBECONFIG=$PWD/contrib/tilt/.secret/kcp/tilt-dev-admin.kubeconfig

kubectl get workspaces
kubectl get shards.core.kcp.io
kubectl get logicalcluster
```

No port-forward and no extra envoy config. The credential is minted by the
kcp-operator Kubeconfig controller the deployer runs: the resource creates a
`Kubeconfig` CR naming the front proxy, user `tilt-admin` and group
`system:kcp:admin`, and the controller issues the cert and writes a ready-to-use
kubeconfig into a Secret.

It is deliberately a *different* credential from the `dev-provisioner` Kubeconfig
the deployer mints for itself — sharing that one would make hand-run commands
indistinguishable from the controller's own writes, and expiring it to clean up
would break the running operator.

The front proxy's name is generated (`dev-fp-0fdfeb` — `names.FrontProxy()`
hashes the PlatformMesh, front proxy and cluster ID into it), so the resource
selects it by the `deploy.platform-mesh.io/{platform-mesh,component}` labels the
deployer stamps on everything it generates.

Set `PLATFORM_MESH_NAME` to rename the installation (it prefixes the engage
Secret and every generated shard, so it renames the tree rather than adding one).

## Layout

```
contrib/tilt/
  Tiltfile              # entrypoint: config, addressing, local infra, kcp, profiles
  infra_remote.Tiltfile # ext:// + remote-chart infra (gated by TILT_NO_INFRA)
  helpers.py            # chart_path(), component_binary(), component_build()
  manifests/            # local, no-fetch infra manifests (namespace, issuer, dex)
  runtime.Dockerfile    # thin image for hot-reloaded binaries
  bin/ .cache/ .secret/ # gitignored working dirs
```

Per-component wiring lives **with the component** — e.g.
`operators/platform-mesh-deployer/Tiltfile`. This Tiltfile only decides whether to
deploy them and supplies what is environment-wide. Because those files are loaded
with `load_dynamic()`, their relative paths resolve against *this* directory,
which is why they take `repo_root` and are handed `component_build` /
`component_binary` rather than loading `helpers.py` themselves.

`component_build()` deploys a component from its production Helm chart.
`component_binary()` is the build half on its own — same compile, image and
live_update sync — for a component whose manifests are not a chart;
`platform-mesh-deployer` uses it and then applies its own `config/default`
kustomization.

`manifests/dex.yaml` carries `__CLUSTER__`/`__PORT__` placeholders that the
Tiltfile substitutes. The issuer URL, the serving certificate's SAN and the
TLSRoute hostname must all agree with each other *and* with what the shards
verify against, so they come from one pair of values rather than four literals.
