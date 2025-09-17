> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# platform-mesh - security-operator
![build status](https://github.com/platform-mesh/security-operator/actions/workflows/pipeline.yaml/badge.svg)

## Description
The platform-mesh security-operator is the component responsible for security configuration. It automaticly configures and updates isolated authorization models for platform mesh utializing OpenFGA, KeyClock and KCP.

It consists of 3 parts: initializer, generator and security-operator.
- Initializer will be triggered when a new workspace with workspace type which extends "security" workspace type appears. It reconciles this new workspase and creates store in OpenFGA, add a new realm with a client, etc.
- Generator reconciles apibinding resource from kcp and generates OpenFGA model for it
- Security-operator reconciles store and authorization model resources from kcp


## Features
- Stores, tupels and authorization models management in OpenFGA
- Instantiation of Stores and authorization models resources in KCP
- KeyClock realms and clients management in Keyclock
- Instantiation of Realms and Clients resources in deployment cluster

## Getting started

- For running and building the security-operator, please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository.
- To deploy the security-operator to kubernetes, please refer to the [helm-charts](https://github.com/platform-mesh/helm-charts) repository. 

## Releasing

The release is performed automatically through a GitHub Actions Workflow.

All the released versions will be available through access to GitHub (as any other Golang Module).

## Requirements

The security-operator requires a installation of go. Checkout the [go.mod](go.mod) for the required go version and dependencies.

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/platform-mesh/extension-manager-operator/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Contributing

Please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository for instructions on how to contribute to platform-mesh.

## Code of Conduct

Please refer to the [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) file in this repository informations on the expected Code of Conduct for contributing to platform-mesh.

## Licensing

Copyright 2024 SAP SE or an SAP affiliate company and platform-mesh contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/platform-mesh/security-operator).
