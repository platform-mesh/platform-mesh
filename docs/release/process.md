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
| Third-party software (kcp, OpenFGA, Traefik, …) | their own | [`ocm/versions.yaml`](../../ocm/versions.yaml) |
| Go modules other projects import (`apis`, `golang-commons`, `subroutines`) | their own SemVer | git tag `<module>/vX.Y.Z` on demand, nothing is built ([why](#go-modules)) |

## Versions live in tags, not in files

Every `charts/*/Chart.yaml` says `version: 0.0.0`, and the charts of components
built here also `appVersion: 0.0.0`. The real version is written in at build
time by [`hack/release/stage-charts.sh`](../../hack/release/stage-charts.sh)
(into a copy under `ocm/.generated/charts/`, the working tree stays clean).
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

2. Test the candidate: install it as described in
   [`installation/README.md`](../../installation/README.md) with
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
   is built for amd64 and arm64, pushed as
   `ghcr.io/platform-mesh/platform-mesh/<image>:vYY.MM.P` (image tags keep the
   `v`, like before; OCM component versions never had one), cosign-signed, its
   SBOMs attested, and published as the signed OCM component
   `github.com/platform-mesh/images/<component>:YY.MM.P`
   ([`hack/release/build-image-component.sh`](../../hack/release/build-image-component.sh)).
2. **aggregate** – [`hack/release/build-aggregate.sh`](../../hack/release/build-aggregate.sh)
   packages the charts at `YY.MM.P`, pushes them to
   `ghcr.io/platform-mesh/helm-charts/<chart>` (where they always were), builds the OCM graph (chart
   components, service components, third-party wrappers, aggregate), signs
   the new component versions and publishes them to `ghcr.io/platform-mesh`.
   Every referenced component must exist: the images from step 1, the
   components from other repositories at their `appVersion`, the third-party
   wrappers from `ocm/versions.yaml`. The run fails with a list otherwise.
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

Install it like a release ([`installation/README.md`](../../installation/README.md))
with `--set-string ocm.version=26.11.0-dev.42.g1a2b3c4`.

Prerelease versions older than two weeks are deleted by
[`prerelease-cleanup.yml`](../../.github/workflows/prerelease-cleanup.yml).

## Changing things

- **A component or its chart**: just change it. The next release ships it.
  CI runs the chart lint and unit tests; there is no version to bump.
- **A component from another repository** (portal, …): that repository
  releases its image and OCM component `github.com/platform-mesh/images/<name>`
  as before; then bump `appVersion` in `charts/<name>/Chart.yaml` here.
- **Third-party software**: change the pin in `ocm/versions.yaml` (Renovate
  proposes these). Wrapper components are versioned by their `PM_*` entry there
  and are immutable like everything else — bump it when a wrapper's content
  changes.
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
├─ kcp               → github.com/kcp-dev/kcp:<PM_KCP_VERSION>              (wrapper around upstream artifacts)
└─ ...
```

The names of the components and of the references are a contract with the
platform-mesh-operator and with everyone installing from our registry; do not
rename them. The graph is generated from
[`ocm/component-constructor.yaml`](../../ocm/component-constructor.yaml)
(aggregate and third-party wrappers), `ocm/components.yaml` (which charts
belong to components built here) and
[`ocm/charts/<chart>.yaml`](../../ocm/charts/README.md) (per-chart deviations
from the default shape).

## Tooling

The release scripts need `ocm` (the CLI from
[open-component-model/open-component-model](https://github.com/open-component-model/open-component-model)),
`helm` and `yq`. Their versions are pinned in `tools/Taskfile.yaml`
(`task tools:ocm` etc. install them into `bin/`). Scripts source
`hack/release/tools.sh` and call `require_tools ocm helm`, which installs the
tools they need and exports their paths as `$OCM`, `$HELM`, ...; the workflows
do the same via `UGET_PRINT_PATH=absolute task tools:<tool>`. So one pin
serves developers and CI. Set `RELEASE_TOOLS_FROM_PATH=true` to use whatever
is on `PATH` instead.

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
| repository variable | `OCM_MIRROR_FROM` | test setups only: OCM repository to copy components from other repositories from |

Image signing uses keyless cosign (GitHub OIDC); no secret.

## Building the graph locally

The workflows have no logic of their own; it is all in `hack/release/`, so it
can be run from a checkout:

```sh
hack/release/build-aggregate.sh --version 26.11.0 --generate-only   # stage charts, write ocm/.generated/constructor.yaml
hack/release/build-aggregate.sh --help
hack/release/next-version.sh                                       # what the next release would be
```

`--image-version` lets a build reuse the images of an existing release (the
charts are then packaged from the working tree, the images are not rebuilt).
