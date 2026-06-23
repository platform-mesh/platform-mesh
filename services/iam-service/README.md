> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# Platform Mesh - iam-service

## Description

The platform-mesh iam-service offers a graphql API for user management. The service then takes management actions to OpenFGA and the Identity Provider. Its design is prepared to allow for alternative implementations to support different Identity Providers. Initially it comes with Keycloak support.

## Features
- GraphQL API for user and role management
- Management of Tuples in OpenFGA
- Multi-cluster resource coordination via kcp
- Keycloak integration for identity provider support
- JWT token validation against kcp


## Getting Started
- For running and building the iam-service, please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository.
- To deploy the iam-service to kubernetes, please refer to the [helm-charts](https://github.com/platform-mesh/helm-charts) repository.

## Releasing

The release is performed automatically through a GitHub Actions Workflow.
All the released versions will be available through access to GitHub (as any other Golang Module).

## Requirements

The iam-service requires an installation of Go. Checkout the [go.mod](go.mod) for the required Go version and dependencies.

## Contributing

Please refer to the [CONTRIBUTING.md](CONTRIBUTING.md) file in this repository for instructions on how to contribute to Platform Mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
