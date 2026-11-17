> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# Platform Mesh - iam-service

## Description

The platform-mesh iam-service offers a GraphQL API for user management. It
coordinates user and role information with OpenFGA and an identity provider.
The Keycloak admin API remains the default user-directory backend. An opt-in,
mutual-TLS provider-neutral directory client is available through
`--identity-directory-endpoint`; it requires a UID-bound identity projection in
the organization's AccountInfo and fails closed when that binding is missing
or does not match the authenticated issuer.

## Features
- GraphQL API for user and role management
- Management of Tuples in OpenFGA
- Multi-cluster resource coordination via kcp
- Keycloak integration for identity provider support
- Opt-in provider-neutral user-directory reads over authenticated mTLS
- JWT token validation against kcp

The directory integration changes the source of user-directory data only.
IAM/FGA role grants still use the legacy email principal model; enabling the
directory client does not activate subject-based authorization or complete the
identity migration.


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
