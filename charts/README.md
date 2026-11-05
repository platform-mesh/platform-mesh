# Helm charts

The Helm charts that make up a Platform Mesh installation. They came from the
former `platform-mesh/helm-charts` repository (history included) and are
published by every Platform Mesh release, all at the release's version.

- `charts/<chart>/` – one chart per directory:
  - `Chart.yaml` (`version: 0.0.0`, see below)
  - `README.md` generated from `README.md.gotmpl`
  - `values.yaml`
  - `component-constructor.yaml` the OCM component versions it publishes, see the "OCM components" below
- `charts/.docs-templates/` – header and footer for the generated READMEs
- `charts/Taskfile.yaml` – `task charts:lint`, `task charts:test`,
  `task charts:docs`, ...

## Versions

Charts carry no version in git. Platform Mesh has one version for everything
(`docs/release/process.md`); the release workflow writes it into every chart
and into the `appVersion` of charts whose image is built in this repository.
Dependencies on other charts in this directory are `file://` references, also
at `0.0.0`; they are resolved when a chart is packaged or tested. Charts of
components from other repositories (portal, iam-ui, …) keep their `appVersion`
as the pin of that component's image.

Published charts: `oci://ghcr.io/platform-mesh/helm-charts/<chart>:<version>`.

## OCM components

`charts/<chart>/component-constructor.yaml` declares the OCM component versions
a chart contributes to a release.

Library charts to not publish an OCM CV.

Every component constructor must include every docker image it includes
as well as the chart:

```text
github.com/platform-mesh/<chart>:V                      service component
  ├─ chart → github.com/platform-mesh/helm-charts/<chart>:V
  │            resource "chart" (the packaged chart in the OCI registry)
  │            source   "chart" (this repository at the released commit)
  └─ image → github.com/platform-mesh/images/<chart>:<image version>
```

When building the component versions the following variables are
available and expanded by ocm:

| Variable | Value |
|---|---|
| `${PM_VERSION}` | the Platform Mesh version being released, every chart and every component built here carries it |
| `${CHART_VERSION}`, `${APP_VERSION}` | `version` and `appVersion` of the chart as published |
| `${DEP_<NAME>_VERSION}` | the version of each `Chart.yaml` dependency, `DEP_PROMETHEUS_VERSION` for `prometheus` |
| `${CHART_REPO}` | the OCI repository charts are pushed to, usually `ghcr.io/platform-mesh/helm-charts` |
| `${REPO_URL}`, `${COMMIT}` | the repository and commit, recorded as the component's source |
| anything in `charts/<chart>/ocm.yaml` | a value of the chart's `values.yaml`, see below |

Versions of images a chart deploys live in its `values.yaml`, nowhere else.

`charts/<chart>/ocm.yaml` records environment variable names and where
in the charts `values.yaml` to retrieve its value from:

```yaml
envsubst:
  DEX_IMAGE_REGISTRY: .dex.image.registry
  DEX_IMAGE_REPOSITORY: .dex.image.repository
  DEX_IMAGE_TAG: .dex.image.tag
```

Each key is exported with the value at the path before ocm runs on that
chart's constructor; a path without a value fails the build.

The component names and the reference names are used by
platform-mesh-operator, when changing these the respective references in
the pm-operator code must be updated.

## Installing Platform Mesh

See [`installation/README.md`](../installation/README.md): the
`platform-mesh-prerequisites` chart, then `platform-mesh-operator` with
`ocm.enabled=true` and the Platform Mesh version to install. Adding a chart to
the installation is described in
[`docs/deployment/adding-a-service.md`](../docs/deployment/adding-a-service.md).

## Working on charts

```sh
task charts:lint            # chart-testing lint
task charts:test            # helm-unittest
task charts:helmtest-update # refresh snapshots after intended changes
task charts:docs            # regenerate the READMEs
```

Requirements: [helm](https://helm.sh), [ct](https://github.com/helm/chart-testing)
and [helm-unittest](https://github.com/helm-unittest/helm-unittest)
(`task charts:setup:helm-docs` / `task charts:setup:kube-lint` install the helpers).

See [`AGENTS.md`](AGENTS.md) for conventions.
