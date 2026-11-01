# Helm charts

The Helm charts that make up a Platform Mesh installation. They came from the
former `platform-mesh/helm-charts` repository (history included) and are
published by every Platform Mesh release, all at the release's version.

- `charts/<chart>/` – one chart per directory: `Chart.yaml` (`version: 0.0.0`,
  see below), `values.yaml`, templates, `tests/` (helm-unittest) and a
  `README.md` generated from `README.md.gotmpl`
- `charts/.docs-templates/` – header and footer for the generated READMEs
- `charts/Taskfile.yaml` – `task charts:lint`, `task charts:test`,
  `task charts:docs`, …
- `ocm/` – how the charts are combined into OCM components
  ([`ocm/charts/README.md`](../ocm/charts/README.md))

## Versions

Charts carry no version in git. Platform Mesh has one version for everything
(`docs/release/process.md`); the release workflow writes it into every chart
and into the `appVersion` of charts whose image is built in this repository.
Dependencies on other charts in this directory are `file://` references, also
at `0.0.0`; they are resolved when a chart is packaged or tested. Charts of
components from other repositories (portal, iam-ui, …) keep their `appVersion`
as the pin of that component's image.

Published charts: `oci://ghcr.io/platform-mesh/helm-charts/<chart>:<version>`.

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
