> [!WARNING]
> This Repository is under development and not ready for productive use. It is in an alpha stage. That means APIs and concepts may change on short notice including breaking changes or complete removal of apis.

# platform-mesh - security-operator

## Description
Security-operator is responsible for security related configuration in Platform-mesh.

## API description
- **Store** - serves as CRD representation of OpenFGA store entity. Stores are created during logical clusters initialization phase or at deployment phase of Platform-mesh installation. When created, dedicated controller will create a **store** in OpenFGA.
- **AuthorizationModel** - serves as CRD representation of OpenFGA Authorization model entity. AuthorizationModels are created when not default ApiBinding is created in the user's workspace. When created, dedicated controller will update Authorization model in the related store in OpenFGA.
- **Invite** - serves as a mechanism for inviting people in your organization by their email
- **IdentityProviderConfiguration (IDP)** - CRD for realm configuration in Keycloak and OIDC clients management. IDP is created during logical clusters initialization phase or at deployment phase of Platform-mesh installation.
- **ApiExportPolicy** - CRD for granting **bind** permissions. When provider creates an API to share this API with other customers of Platform-mesh, he needs to get **bind** permissions and after this other users will be able to bind provider's API and use it
- **ProviderPermissions** - configures roles and permission expressions for resources in a provider's APIExport.

### ProviderPermissions default permissions

Each `spec.permissions` key uses `{singular}.{group}`, for example
`component.metadata.dxp.sap.com`. Under `defaultPermissions`, the following
Kubernetes verbs can be overridden with OpenFGA expressions:

| Verb | Where the override is evaluated | Default |
| --- | --- | --- |
| `create` | Parent account or namespace | `owner` |
| `list` | Parent account or namespace | `member` |
| `watch` | Parent account or namespace | `member` |
| `get`, `update`, `delete`, `patch` | Resource object | `member` |

Omitting a field or setting it to an empty string preserves its default.
Cluster-scoped resources use `core_platform-mesh_io_account` as their parent;
namespaced resources use `core_namespace`. The generator overrides only the
provider's resource-specific parent relations, such as
`list_metadata_dxp_sap_com_components`.

Collection expressions must reference relations defined on that parent type.
Provider-defined roles and additional permissions are defined on the resource
object, so they cannot be referenced directly by collection expressions. Likewise,
`from parent` in a collection expression refers to the account's or namespace's
own parent. The existing account `authenticated_user` relation can be used for
cluster-scoped resources; using it on namespaces requires that relation to exist
in the namespace model.

For example, with existing account `authenticated_user` assignments, this gives
authenticated users GET, LIST and WATCH access to cluster-scoped components while
preserving the defaults for create, update, delete and patch:

```yaml
apiVersion: providers.platform-mesh.io/v1alpha1
kind: ProviderPermissions
metadata:
  name: metadata
spec:
  apiExport:
    ref:
      name: metadata.dxp.sap.com
  permissions:
    component.metadata.dxp.sap.com:
      defaultPermissions:
        get: authenticated_user from parent or member
        list: authenticated_user or member
        watch: authenticated_user or member
```

LIST and WATCH authorize the whole collection in the account or namespace. They
do not filter objects by their individual GET permissions, so configure GET
separately when users should also be able to read individual objects.

The `watch` override now controls the parent relation checked by Kubernetes watch
requests. When configured, the object-level `watch` relation inherits this parent
permission; when omitted, its existing `member` default is retained. Previously,
the override changed only the object-level relation and did not affect Kubernetes
watch authorization. Existing overrides that reference resource-local roles or
relations must be rewritten as parent expressions before upgrading.

## Features
- **Initialization of logical clusters** - This feature consist of 2 parts:
    - **organization level logical clusters** - operator creates **Store**, **IDP**, **Invite**, **WorkspaceAuthenticationConfiguration** resources to initialize an organization
    - **account level logical clusters** - operator creates additional tuples in organization's store for accounts hieracy
- **Authorization Model generation** - to execute authorization checks in OpenFGA against custom resource which are created by the user, operator generates Authorization Model for each resource in ApiExport when the ApiExport is bound (ApiBinding is created). The model is created in the workspace where **ApiExport** and **ApiResourceSchema** resource live.
- **OIDC management** - Keycloak serves as the internal Identity Provider within Platform Mesh. After IDP resource is created and reconciled successfully, **WorkspaceAuthenticationConfiguration** resource is created and configured to use keycloak as identity provider for kcp authentication
- **ApiExport bindability control** - ApiExportPolicy controller creates all necessary tuples in OpenFGA to support authorization checks for **bind** kcp's verb. More information about this [ApiExportPolicy ADR](https://github.com/platform-mesh/architecture/blob/main/adr/002-apiexport-binding-access-control.md)
- **Reconcile logical cluster** - security-operator reconciles logical clusters after they are initialized and applies the same logic as initializer does. It keeps already initialized logical clusters up to date if something has been changed in initializing flow.

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

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
