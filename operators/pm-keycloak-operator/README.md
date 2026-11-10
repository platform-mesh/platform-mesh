> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# platform-mesh - pm-keycloak-operator

## Description

pm-keycloak-operator manages the Keycloak identity provider of a Platform Mesh
installation. It owns the `keycloak.platform-mesh.io` API group and serves the
deprecated `core.platform-mesh.io` kinds of the same shape while the
`LegacyAPIs` feature gate is on.

## API description

- **IdentityProviderConfiguration** - realm and OIDC client configuration in Keycloak.
  Created for every organization during workspace initialization and once for
  the `welcome` realm by platform-mesh-operator. The reconciler registers the
  clients with Keycloak and writes their secrets into `root:orgs`.
- **Invite** - invites a person to an organization by email: a Keycloak user is
  created in the organization's realm and the account-setup email is sent.

## Commands

| Command | Purpose |
| --- | --- |
| `operator` | IdentityProviderConfiguration and Invite reconcilers (engaged through the `keycloak.platform-mesh.io` APIExport), the organization LogicalCluster reconciler and the IdentityProviderConfiguration validating webhook. With `LegacyAPIs=true` also the deprecated core kinds through the `system`/`core` exports. |
| `initializer` | Initializer `root:keycloak` for organization workspaces: creates the IdentityProviderConfiguration, the creator's Invite and the organization's WorkspaceAuthenticationConfiguration. |
| `terminator` | Terminator `root:keycloak`: removes what the initializer created. |
| `init-container` | Bootstraps the Keycloak service-account clients in the `master` realm. |

## Feature gates

`--feature-gates=<Gate>=<bool>`

| Gate | Default | Effect |
| --- | --- | --- |
| `LegacyAPIs` | `true` | Also reconcile and validate the deprecated `core.platform-mesh.io` IdentityProviderConfigurations and Invites. Turn off once an installation is migrated to `keycloak.platform-mesh.io`. |

## Getting started

- For running and building the operator, please refer to the [CONTRIBUTING.md](../../CONTRIBUTING.md) file in this repository.
- To deploy the operator to kubernetes, please refer to the [helm-charts](../../charts/pm-keycloak-operator).

## Requirements

pm-keycloak-operator requires an installation of go. Checkout the [go.mod](go.mod) for the required go version and dependencies.

## Contributing

Please refer to the [CONTRIBUTING.md](../../CONTRIBUTING.md) file in this repository for instructions on how to contribute to platform-mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
