> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# Platform Mesh - rebac-authz-webhook

## Description

The Platform Mesh IAM Authorizaton Webhook is a kubernetes authorization webhook that uses openFGA to answer authorization requests from kubernetes.

## kcp Configuration

The webhook requires access to the root kcp API Server (not Virtual Workspace) for API Server Discovery. This is necessary because:

- `cache.New()` inside the provider needs root kcp API Server to discover `APIExportEndpointSlice` resources
- Virtual Workspace kubeconfig doesn't support API Server Discovery for root kcp CRDs like `APIExportEndpointSlice`
- The provider will then use Virtual Workspace URLs from the endpoint slice for actual cluster access

The webhook uses `ctrl.GetConfigOrDie()` which respects the `KUBECONFIG` environment variable. The Helm chart deployment template sets this environment variable to point to a kubeconfig secret provided by the platform-mesh-operator that contains the root kcp API Server URL.

The default `apiExportEndpointSliceName` is `"core.platform-mesh.io"` (configured in the code). This can be overridden via the `--kcp-api-export-endpoint-slice-name` command-line argument if needed.

## Releasing

The release is performed automatically through a GitHub Actions Workflow.

All the released versions will be available as packages on this GitHub repository.

## Requirements

To build an run the webhook locally a installation of go is required. Checkout the [go.mod](go.mod) for the required go version.
In order to build the Dockerfile a compatible tooling like docker or podman is required.

## Contributing

Please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository for instructions on how to contribute to Platform Mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

## Licensing

Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available via the [REUSE tool](https://api.reuse.software/info/github.com/platform-mesh/platform-mesh).

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
