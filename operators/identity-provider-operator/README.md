# Identity provider operator

Independent, staged orchestration for `identity.platform-mesh.io/v1alpha1`.
It does not import or require Keycloak administration clients.

Currently the `admission` command provides provider-neutral validation and
authenticated dispatch to the selected class's optional validator.
`pkg/orchestration` verifies immutable Account/Workspace/LogicalCluster bindings,
maintains aggregate organization identity intent, and prepares observed OIDC/JWT
outputs without publishing authentication. It requires explicitly configured
client callbacks and never infers provider endpoints or adopts foreign tenants.
`pkg/orchestration.Publisher` is a gated, versioned organization-authentication
publication helper. The opt-in `publish-organizations` command watches Account
objects in the trusted organizations workspace and invokes it using a private
activation Secret and explicit client configuration JSON. It stamps new
tenants, refuses implicit adoption of existing tenants and prepares immutable
activation-scoped authentication before switching workspace types. The command
is not deployed by current charts or profiles. Lifecycle termination, welcome
bootstrap, installation deployment and migration coordination are not complete.
Existing identity writers and authentication defaults remain unchanged.

Use `go run . admission --help` and `go run . publish-organizations --help` for
configuration. Admission serving certificate/key, KCP caller CA, explicit
SPIFFE workload allowlist and trusted installation workspace are required.
Publication requires the trusted class and organization workspaces, stable
installation ID, private activation Secret and an administrator-supplied JSON
client configuration file.

See [the identity contract](../../docs/identity-provider-contract.md) for
protocols, default-provider qualification and mandatory activation gates.

Development: `task test`, `task lint`, `task build`.
