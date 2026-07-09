# Platform Mesh — Tilt local development environment

Tilt replaces the OCM/Flux/platform-mesh-operator delivery pipeline for the
developer inner loop: static infrastructure is deployed once into kind, and the
operators/services you are working on hot-reload in seconds.

## What it deploys today

- **Local infra** (`manifests/`): `platform-mesh-system` namespace, a
  self-signed cert-manager `Issuer`, an envoy `Gateway` named `platform-mesh`,
  and a dev etcd.
- **Remote infra** (`infra_remote.Tiltfile`, skipped by `TILT_NO_INFRA=1`):
  cert-manager, the envoy gateway controller, kcp-operator.
- **kcp** (static): the upstream kcp Tilt module `deploy_kcp()`, parameterized
  for our gateway, hostnames (`root.pm.localhost`), OIDC issuer and (in
  `auth`/`full`) the ReBAC authorization webhook. kcp always runs a **pinned
  released image** — it is never built from source here.

## Prerequisites

- docker or podman, `kind`, `kubectl`, `helm`, `tilt`, `git`
- The kcp static-install module (`deploy_kcp`) is fetched automatically from the
  kcp repo — **no local kcp checkout required**. Until the upstream PR merges it
  comes from the in-flight branch (`load_kcp()` in `helpers.py`). Overrides:
  - `KCP_TILT_DIR` — path to a local `kcp/contrib/tilt` checkout; skips the
    fetch (use offline or when hacking on the module itself).
  - `KCP_TILT_REPO` / `KCP_TILT_REF` — git URL / ref to fetch from.

## Usage

```sh
# create the kind cluster first — it MUST be named platform-mesh.
# The Tiltfile refuses any context other than kind-platform-mesh (guard against
# a stray `tilt up` hitting a shared/other cluster). Override with
# TILT_ALLOWED_CONTEXT if you must.
kind create cluster --name platform-mesh
tilt up -f contrib/tilt/Tiltfile -- --profile=core 

HELM_CHARTS_DIR=/Users/mjudeikis/go/src/github.com/platform-mesh/helm-charts \
KCP_TILT_DIR=/Users/mjudeikis/go/src/github.com/kcp-dev/kcp/contrib/tilt \
tilt up -f contrib/tilt/Tiltfile -- --profile=core
```

### Validate manifests without a cluster

`deploy_kcp()`'s output can be inspected offline — no cluster, no remote
fetches:

```sh
TILT_NO_INFRA=1 tilt alpha tiltfile-result -f contrib/tilt/Tiltfile
```

This renders the local manifests and the kcp custom resources (RootShard,
FrontProxy, TLSRoutes, Kubeconfigs) with all hooks applied, so you can confirm
the gateway wiring, OIDC issuer and authorization webhook before deploying.

## The kcp hooks

The kcp static install lives upstream in
`kcp-dev/kcp/contrib/tilt/kcp_static.Tiltfile` and exposes `deploy_kcp()`. This
project owns its own infrastructure (gateway, etcd, cert issuer) and passes it
in. Hooks exercised here:

| Hook | Purpose |
|---|---|
| `gateway` + `route_version` | attach kcp TLSRoutes to our `platform-mesh` gateway (prod: traefik) |
| `authorization_webhook_secret` | wire the ReBAC authz webhook (L3, `auth`/`full`) |
| `oidc` | front kcp with dex/keycloak as the OIDC issuer |
| `image_tag` / `image_repo` | pin the released kcp image |
| `extra_shards` | opt-in extra shards (`--sharded`) |
| `namespace`, `base_domain`, `etcd_endpoint`, `issuer_ref` | environment wiring |

## Layout

```
contrib/tilt/
  Tiltfile              # entrypoint: config, local infra, kcp, components
  infra_remote.Tiltfile # ext:// + remote-chart infra (gated by TILT_NO_INFRA)
  helpers.py            # chart_path(), component_build()
  manifests/            # local, no-fetch infra manifests
  runtime.Dockerfile    # thin image for hot-reloaded binaries
  bin/ .cache/ .secret/ # gitignored working dirs
```
