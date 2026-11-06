# Releasing Platform Mesh

Platform Mesh has one version. A release is a git tag `vYY.MM.P` on this
repository (CalVer: year, month, patch — `v26.10.0` is the October 2026
release, `v26.10.1` its first patch release); everything that is built from the repository — every component
image, every Helm chart, every OCM component and the aggregate
`github.com/platform-mesh/platform-mesh` — is built from that commit and
carries that version. There is nothing to prepare before tagging and no
component is released on its own. The reasoning is in
[RFC 015](https://github.com/platform-mesh/architecture/blob/main/rfc/015-versioning.md).

| What | Version | Source |
|---|---|---|
| Component images (account-operator, iam-service, …) | `YY.MM.P` | built from the tagged commit |
| Helm charts and their OCM components | `YY.MM.P` | packaged from the tagged commit (`0.0.0` in git, see below) |
| The aggregate `github.com/platform-mesh/platform-mesh` | `YY.MM.P` | |
| Components from other repositories (portal, iam-ui, marketplace-ui, example-httpbin-operator) | their own | `appVersion` in `charts/<name>/Chart.yaml` |
| Third-party software (kcp, OpenFGA, Traefik, …) | their own | `third_party/<name>/component-constructor.yaml` |
| Go modules other projects import (`apis`, `golang-commons`, `subroutines`) | their own SemVer | git tag `<module>/vX.Y.Z` on demand, nothing is built ([why](#go-modules)) |

## Versions live in tags, not in files

Every `charts/*/Chart.yaml` says `version: 0.0.0`, and the charts of components
built here also `appVersion: 0.0.0`. The real version is passed at build time:
[`hack/release/ocm-build-chart`](../../hack/release/ocm-build-chart/main.go)
runs `helm package --version YY.MM.P` on the chart directory, with
`--app-version vYY.MM.P` where the chart says `0.0.0` (that is the image tag
the chart defaults to). Nothing is copied or rewritten.
Chart dependencies on `common` and the `*-crds` charts are `file://` references
into this directory, pinned to the same version.

The only version-related file is [`VERSION`](../../VERSION) at the repository
root. It holds the feature release (`26.11`) that the branch is working
towards and is bumped on `main` by hand right after a feature release. Release
branches keep theirs.
[`hack/release/next-version.sh`](../../hack/release/next-version.sh) derives
the next full version from it and from the tags reachable from `HEAD`.

## Releasing

### A feature release

1. On `main`, cut a release candidate:

   If you're unsure what to tag, you could run `hack/release/next-version.sh` now,
   but usually you know what to tag and can simply go ahead:

   ```sh
   git tag v26.11.0-rc.1 && git push origin v26.11.0-rc.1
   ```

2. Test the candidate: install it as described in the
   [installation guide](https://platform-mesh.github.io/main/how-to-guides/install-platform-mesh) with
   `--set-string ocm.version=26.11.0-rc.1` (and the operator chart at the same
   version). Fixes land on `main`; cut further candidates as needed (`-rc.2`, …).
3. Promote: tag the same version without the `-rc.N`:

   ```sh
   git tag v26.11.0 && git push origin v26.11.0
   ```

4. Afterwards, on `main`: create the release branch and bump `VERSION`:

   ```sh
   git branch release-26.11 v26.11.0 && git push origin release-26.11
   echo 26.12 > VERSION   # commit via pull request
   ```

   When to create the branch is a workflow decision, not a technical one. It
   can also be created earlier (then fixes for the release are cherry-picked
   into it) or later (then `main` is in code freeze until the release). See
   RFC 015.

5. Run `task deps:post-release` to update all dependencies. This may
   need manual fixes to the code.

### A patch release

Fixes are merged to `main` first, then cherry-picked into the release branch
(via a pull request against `release-YY.MM`; no edits needed). Then:

```sh
git checkout release-26.11
git tag v26.11.1 && git push origin v26.11.1
```

### What the tag triggers

[`release.yml`](../../.github/workflows/release.yml) calls
[`job-publish.yml`](../../.github/workflows/job-publish.yml) with the version
from the tag:

1. **images** – every component in [`ocm/components.yaml`](../../ocm/components.yaml)
   with an image is built for amd64 and arm64, together with the images under
   `<component>/images/<name>/`, pushed as
   `ghcr.io/platform-mesh/platform-mesh/<image>:vYY.MM.P` (image tags keep the
   `v`, like before; OCM component versions never had one), cosign-signed, its
   SBOMs attested, and published by
   [`hack/release/ocm-build-image`](../../hack/release/ocm-build-image/main.go)
   as the signed OCM component declared in the component's
   `component-constructor.yaml`, `github.com/platform-mesh/images/<component>:YY.MM.P`.
2. **aggregate** – one command per directory, each building the directory's
   `component-constructor.yaml` into `ghcr.io/platform-mesh` and signing what
   it built:
   [`ocm-build-third-party`](../../hack/release/ocm-build-third-party/main.go)
   for every `third_party/<name>/`,
   [`ocm-build-chart`](../../hack/release/ocm-build-chart/main.go) for every
   `charts/<chart>/` (packages the chart at `YY.MM.P`, pushes it to
   `ghcr.io/platform-mesh/helm-charts/<chart>`, then the chart and service
   components), and
   [`ocm-build-aggregate`](../../hack/release/ocm-build-aggregate/main.go)
   for the root constructor (copies etcd-druid from Gardener's registry first).
   Every referenced component must exist: the images from step 1, the
   components from other repositories at their `appVersion`, the third-party
   wrappers. ocm fails on the first missing reference and names it.
3. **verify** – resolves the published graph, checks every signature of this
   version, pulls and templates charts.
4. **trivy** – scans the images and SBOMs.
5. `release.yml` then generates the release notes
   ([`hack/release/release-notes.sh`](../../hack/release/release-notes.sh):
   merged pull requests since the previous tag, grouped by area; changed
   third-party and other-repository versions) and creates the GitHub release,
   marked as prerelease for `-rc.N` tags.

Published versions are immutable. Tagging a version that exists in the
registry already fails; use the next one.

### Prereleases from main

To publish the current `main` without a release, run the
[`prerelease`](../../.github/workflows/prerelease.yml) workflow (Actions →
prerelease → Run workflow). It runs the same `job-publish.yml` with the version
`<next>-dev.<run>.g<sha>`, e.g. `26.11.0-dev.42.g1a2b3c4` (`dev` sorts below
`rc`), and creates no GitHub release. The version is in the run's summary:

Install it like a release ([installation guide](https://platform-mesh.github.io/main/how-to-guides/install-platform-mesh))
with `--set-string ocm.version=26.11.0-dev.42.g1a2b3c4`.

Prerelease versions older than two weeks are deleted by
[`prerelease-cleanup.yml`](../../.github/workflows/prerelease-cleanup.yml).

## Changing things

- **A component or its chart**: just change it. The next release ships it.
  CI runs the chart lint and unit tests; there is no version to bump.
- **A component from another repository** (portal, …): that repository
  releases its image and OCM component `github.com/platform-mesh/images/<name>`
  as before; then bump `appVersion` in `charts/<name>/Chart.yaml` here.
- **Third-party software**: change the pin in
  `third_party/<name>/component-constructor.yaml` (Renovate proposes these).
  Wrapper components are versioned with the PM version and are immutable like
  everything else.
- **A new component**: see [adding a service](../deployment/adding-a-service.md).
- **A Go module** (`apis`, …): see below.

### Go modules

`apis`, `golang-commons` and `subroutines` are the exception to "one version":
they keep their own SemVer tags (`apis/v0.2.0`), created by hand when a
consumer outside this repository needs a new version. Then
`task bump-deps -- apis v0.2.0` points the in-repo `go.mod` files at it.
Nothing is built or published; a tag is all a Go module release is.

Why they do not get `apis/vYY.MM.P` with every release:

- A release does not need them. Inside this repository every component
  resolves the three modules through `go.work`, i.e. from the working tree;
  the versions in the `go.mod` files only matter to external consumers.
- Go ties the major version to the import path: anything at v2 or above must
  be imported as `go.platform-mesh.io/apis/v26/...`, so CalVer tags would
  force a new import path for every consumer each January, whether the Go
  API changed or not. Staying on SemVer avoids that.

Consumers who want "the apis that shipped with 26.10.0" look at the
`go.platform-mesh.io/apis` line in any component's `go.mod` at the `v26.10.0`
tag.

## The OCM component graph

```
github.com/platform-mesh/platform-mesh:X.Y.Z
├─ account-operator  → github.com/platform-mesh/account-operator:X.Y.Z
│                        ├─ chart → github.com/platform-mesh/helm-charts/account-operator:X.Y.Z
│                        └─ image → github.com/platform-mesh/images/account-operator:X.Y.Z
├─ portal            → github.com/platform-mesh/portal:X.Y.Z
│                        ├─ chart → .../helm-charts/portal:X.Y.Z
│                        └─ image → .../images/portal:<appVersion>          (built by the portal repository)
├─ infra             → github.com/platform-mesh/infra:X.Y.Z
│                        ├─ chart → .../helm-charts/infra:X.Y.Z
│                        └─ image → .../images/infra:X.Y.Z                  (third-party images the chart deploys)
├─ kcp               → github.com/kcp-dev/kcp:<X.Y.Z>                       (wrapper around upstream artifacts)
└─ ...
```

The names of the components and of the references are a contract with the
platform-mesh-operator and with everyone installing from our registry; do not
rename them. The graph is declared in constructors next to what they
describe: the aggregate in [`component-constructor.yaml`](../../component-constructor.yaml)
at the repository root, chart and service components in
`charts/<chart>/component-constructor.yaml` (with `charts/<chart>/ocm.yaml`
mapping values of `values.yaml` to variables, see
[`charts/README.md`](../../charts/README.md)), image components in
`operators/<name>/component-constructor.yaml` and
`services/<name>/component-constructor.yaml`, third-party wrappers in
`third_party/<name>/component-constructor.yaml`. ocm expands the `${VAR}`
placeholders from the environment the commands set.

## Tooling

The release commands are Go programs under `hack/release/` (part of the root
module, component `root` in `ocm/components.yaml`). They call `ocm` (the CLI
from [open-component-model/open-component-model](https://github.com/open-component-model/open-component-model))
and `helm` from `PATH`. Their versions are pinned in `tools/Taskfile.yaml`:
`task tools:ocm tools:helm` installs them into `bin/`, which the workflows put
on `PATH`. So one pin serves developers and CI. The remaining shell scripts
(`hack/charts/*.sh`) source `hack/release/tools.sh` and call
`require_tools`, which does the same install; set
`RELEASE_TOOLS_FROM_PATH=true` to use whatever is on `PATH` instead.

## Verifying a release

```sh
ocm get component-version ghcr.io/platform-mesh//github.com/platform-mesh/platform-mesh:26.11.0 --recursive
ocm verify component-version --signature platform-mesh.platform-mesh \
  ghcr.io/platform-mesh//github.com/platform-mesh/platform-mesh:26.11.0   # public key in ~/.ocmconfig
helm pull oci://ghcr.io/platform-mesh/helm-charts/account-operator --version 26.11.0
cosign verify ghcr.io/platform-mesh/platform-mesh/account-operator:v26.11.0 \
  --certificate-identity-regexp 'https://github.com/platform-mesh/platform-mesh/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Secrets and configuration

| Where | Name | Purpose |
|---|---|---|
| repository secret | `OCM_SIGNING_PRIVATE_KEY`, `OCM_SIGNING_CERT` | RSA key and certificate for OCM signatures. The signature name must equal the certificate's CN. |
| repository secret | `PM_PUBLISHER_PRIVATE_KEY` (planned) | private key of the "PM Publisher" GitHub App, to push to the `helm-charts/*` packages created by the former helm-charts repository. The fork uses `GITHUB_TOKEN`; see `cutover.md`. |
| repository variable | `OCM_SIGNATURE_NAME` | signature name, default `platform-mesh.platform-mesh` |

Image signing uses keyless cosign (GitHub OIDC); no secret.

## Building the graph locally

The workflows have no logic of their own; it is all in `hack/release/`, so it
can be run from a checkout. The commands take their configuration from the
environment (`PM_VERSION` is required; `OCM_REPO` defaults to
`ghcr.io/platform-mesh`, a `ctf::<path>` value builds into a local archive
instead; `SIGNATURE` signs; the header comment of each `main.go` lists the
rest) and one directory argument where one is needed:

```sh
export PATH=$PWD/bin:$PATH PM_VERSION=26.11.0 OCM_REPO=ctf::/tmp/pm.ctf
go run ./hack/release/ocm-build-third-party third_party/kcp
SKIP_PUSH=true go run ./hack/release/ocm-build-chart charts/account-operator   # SKIP_PUSH: the chart archive is already published
go run ./hack/release/ocm-build-aggregate
ocm get component-version "$OCM_REPO//github.com/platform-mesh/platform-mesh:$PM_VERSION" --recursive
hack/release/next-version.sh                                                    # what the next release would be
```

A chart's component references the image component at the same version, so a
local build of a chart needs the image component in `OCM_REPO` first
(`ocm-build-image <component dir> <sbom dir>`, with
`<sbom dir>/<image>/sbom-{cyclonedx,spdx}.json`), and the components
published by other repositories (portal, iam-ui, …) exist only in
`ghcr.io/platform-mesh`.
