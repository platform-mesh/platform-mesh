> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# Platform Mesh - Extension Manager Operator

## Description

The extension-manager-operator implements the lifecycle management of a Kubernetes CRD `ContentConfiguration` resource, which is a Kubernetes Resource/API for configuration of Micro Frontends in Platform Mesh.

For reference, see the [RFC for Platform Mesh Extension Management - CDM Processing](https://github.com/platform-mesh/architecture/blob/main/rfc/002-extension-content-configuration-processing.md).

## Features
- Support for inline and remote content configurations.
- Validation of content configuration and generation of a JSON Schema that can be used by contributors to validate their content configuration.
- Services to allow validation of content configuration at runtime while developing a micro frontend on the developers system.
- Ability to provide validation feedback while keeping the last validated content configuration.

## KCP vs. K8S

The operator always runs with **multicluster-runtime** and optionally reconciles against an APIExportEndpointSlice using the KCP APIExport provider. The relevant three configuration parameters are:

* The `KUBECONFIG` environment variable will be used for reconciling, defaulting to `ctrl.GetConfigOrDie()`.
* The optional `--leader-elect` CLI switch enables leader election and will use `rest.InClusterConfig()` for election.
* The opitonal `--kcp-api-export-endpoint-slice-name` enables the APIExport provider.

In practice this means:
* To reconcile against a KCP instance, configure `KUBECONFIG` to point to KCP and set `--kcp-api-export-endpoint-slice-name` accordingly. Optionally enable `--leader-election` when running as Pod in a k8s cluster to use in-cluster leader election.
* To reconcile against plain K8S and running as Pod in k8s, optionally enable `--leader-election` and leave the the other two variables unset.

## Getting Started
For running Platform Mesh locally checkout our [getting started guide](https://platform-mesh.github.io/platform-mesh.org/docs/getting-started). The extension-manager-operator can be deployed on a kubernetes cluster using the helm-chart [here](https://github.com/platform-mesh/helm-charts/tree/main/charts/extension-manager-operator) and for CRDs [here](https://github.com/platform-mesh/helm-charts/tree/main/charts/extension-manager-operator-crds).

## Releasing

The release is performed automatically through a GitHub Actions Workflow. New Versions will be updated in the helm-chart of the extension-manager-operator located [here](https://github.com/platform-mesh/helm-charts/tree/main/charts/extension-manager-operator). There is a separate helm chart for the extension-manager-operator CRDS located [here](https://github.com/platform-mesh/helm-charts/tree/main/charts/extension-manager-operator-crds).

## Requirements

The extension-manager-operators an installation of go. Checkout the [go.mod](go.mod) for the required go version and dependencies.

## Support, Feedback, Contributing
This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/platform-mesh/platform-mesh/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/platform-mesh/platform-mesh/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Contributing

Please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository for instructions on how to contribute to Platform Mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
