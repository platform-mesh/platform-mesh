> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# platform-mesh - provider-operator

## Description

provider-operator onboards providers into Platform Mesh. It is being split out of
[platform-mesh-operator](../platform-mesh-operator) and is not deployed yet: it has no
image and no Helm chart.

## API description

- **ManagedProvider** - a provider that Platform Mesh installs and runs, created on the
  runtime cluster. The reconciler creates the matching Provider in kcp, waits for it to
  become ready, copies its kubeconfig Secret to the runtime cluster and deploys
  `spec.runtimeDeployments` through Flux.
- **Provider** - a provider in kcp. The reconciler creates its workspace under
  `root:providers` and a kubeconfig scoped to that workspace. Not moved yet:
  platform-mesh-operator still reconciles it.

## Status

- The ManagedProvider controller is a copy of platform-mesh-operator's. Until that copy is
  removed, fix bugs in both.
- Do not run both ManagedProvider controllers against the same cluster. Start
  platform-mesh-operator with `--controllers-managed-provider-enabled=false` to switch its
  one off.

## Getting started

- For running and building the operator, please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file.
- `go run . operator --help` lists the flags. Names and defaults are the same as in platform-mesh-operator.

## Requirements

provider-operator requires an installation of go. Checkout the [go.mod](go.mod) for the required go version and dependencies.

## Contributing

Please refer to the [CONTRIBUTING.md](../../CONTRIBUTING.md) file in this repository for instructions on how to contribute to platform-mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
