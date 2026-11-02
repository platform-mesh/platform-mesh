# End-to-end testing of a checkout

`hack/e2e/` builds this checkout the way a release is built, installs it on a
kind cluster and runs the Playwright smoke test (`tests/`) against it. Nothing
leaves the machine: images and the OCM component graph go to a registry
container next to the cluster. `.github/workflows/e2e.yml` runs the same
scripts for every pull request; locally every step is a task:

```sh
task e2e:pr        # everything below, from an empty machine (~45 min)
task e2e:down      # remove cluster, registry and state
```

| Step | Task | What it does |
|---|---|---|
| 1 | `task e2e:registry` | Starts a `registry:2` container on kind's docker network. Its address (`http://<ip>:5000`) is what everything published refers to. |
| 2 | `task e2e:kind` | Creates the kind cluster of `installation/README.md` and tells containerd on the nodes that the registry speaks plain HTTP. |
| 3 | `task e2e:images` | Builds every component image (`task images-<name>`, as the release does) for the host's architecture and pushes it. `COMPONENTS="iam-service portal"` for a subset. |
| 4 | `task e2e:ocm` | Publishes the image components, the charts, their OCM components, the third-party wrappers and the aggregate with the release scripts (`hack/release/`), signed with a throwaway key. Components from other repositories (portal, iam-ui, ...) are copied from `ghcr.io/platform-mesh`. |
| 5 | `task e2e:verify` | `hack/release/verify-graph.sh`: the graph resolves, every signature verifies, every chart pulls and templates. The release workflow runs the same script against ghcr.io. |
| 6 | `task e2e:install` | Installs the prerequisites and operator charts from the checkout, pointing the operator at the registry, and waits until the portal serves. |
| 7 | `task e2e:test` | The root-shard smoke suite, `task tests:portal-e2e:remote`. |

## Upgrade test

```sh
task e2e:upgrade   # steps 1-5, then: install the latest release, test, upgrade to the checkout, test
```

`task e2e:install-release` installs the latest published release exactly as a
user does (released charts and component from ghcr.io; `RELEASE=26.10.0-rc.3`
for a specific one). `task e2e:install` on top of it is the upgrade: the
operator chart from the checkout moves the OCM Repository to the local
registry and the Component to the snapshot, and the ocm-k8s-toolkit rolls
every chart. The second `task e2e:test` registers a new organisation, so it
does not depend on the first.

## Iterating

The snapshot has a version like `26.11.0-snapshot.1730000000.0.g1a2b3c4`
(`hack/e2e/version.sh`): the next release, a build number, a sequence number
and the commit. Published versions are immutable, so a change needs a new one:

- changed Go code: `task e2e:images e2e:ocm e2e:install` (new build number,
  `COMPONENTS=...` to rebuild only some images);
- changed charts, profiles or `ocm/`: `task e2e:ocm BUMP=true` then
  `task e2e:install` (same images, next sequence number).

`task e2e:install` is `helm upgrade --install`, so it upgrades the running
installation to the new version. Useful while debugging:

```sh
export OCM_CONFIG=.e2e/ocmconfig
bin/ocm-* get component-version "$(hack/e2e/registry.sh url)/platform-mesh//github.com/platform-mesh/platform-mesh:$(task e2e:version)" --recursive
kubectl --context kind-platform-mesh -n platform-mesh-system get components,resources.delivery.ocm.software,helmreleases
task e2e:diagnostics      # .e2e/diagnostics/: PlatformMesh, OCM and Flux objects, events, logs
```

## Requirements

- Docker with container IPs routable from the host: Linux, or OrbStack on
  macOS. Docker Desktop does not route to containers; set `E2E_REGISTRY_URL`
  to an address that resolves both on the host and in the cluster (a
  hostname with an `/etc/hosts` entry) in that case.
- 8 CPUs and 16 GB for the Docker VM; the stack does not fit less.
- `task`, `docker`, `kubectl`, `jq`, `openssl`, `python3`, `node` (for
  Playwright). The pinned `kind`, `helm`, `ocm` and `yq` are installed into
  `bin/` on demand.
- `/etc/hosts` entries for `portal.localhost` and friends; `task e2e:test`
  adds the missing ones with `sudo`.

## How it fits together

The operator never reads charts from disk: it resolves the OCM aggregate from
a registry, Flux pulls the charts as OCI artifacts, and image coordinates come
from the OCM Resource status. So testing a checkout means building its graph
and putting it in a registry the cluster can reach. The release scripts do
that for ghcr.io; here they get `--ocm-repo http://<ip>:5000/platform-mesh`.
The `http://` is the convention for a plain-HTTP registry: the ocm CLI and the
ocm-k8s-toolkit accept it, and the platform-mesh-operator turns it into
`spec.insecure` on the Flux objects it creates. State between the steps
(registry address, build number, image digests, signing key, ocm config) is in
`.e2e/`.
