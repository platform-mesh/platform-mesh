> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# platform-mesh - account-operator
![Build Status](https://github.com/platform-mesh/account-operator/actions/workflows/pipeline.yml/badge.svg)

## Description

The platform-mesh account-operator manages the core Account resource which is a grouping entity in platform-mesh. It manages a related Namespace and will instantiate additional configured resources in its owned Namespace.

## Features
- Account Namespace management
- Instantiation of Account Resource in Namespace
- Support for Spreading Reconciles to improve performance on operator restart****
- Validating webhook to ensure that immutable information is not changed
- Cleanup on Account deletion including namespace cleanup

## Getting started

- For running and building the account-operator, please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository.
- To deploy the account-operator to kubernetes, please refer to the [helm-charts](https://github.com/platform-mesh/helm-charts) repository. 

## Releasing

The release is performed automatically through a GitHub Actions Workflow.

All the released versions will be available through access to GitHub (as any other Golang Module).

## Requirements

The account-operator requires a installation of go. Checkout the [go.mod](go.mod) for the required go version and dependencies.

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/platform-mesh/extension-manager-operator/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Contributing

Please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository for instructions on how to contribute to platform-mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.