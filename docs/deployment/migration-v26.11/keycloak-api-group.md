# Migrate IdentityProviderConfigurations and Invites to keycloak.platform-mesh.io

v26.11 moves the Keycloak integration out of security-operator into
pm-keycloak-operator and gives it its own API group:

| before (deprecated)                                          | after                                                 |
| ------------------------------------------------------------ | ----------------------------------------------------- |
| `core.platform-mesh.io/v1alpha1` `Invite`                    | `keycloak.platform-mesh.io/v1alpha1` `Invite`         |
| `core.platform-mesh.io/v1alpha1` `IdentityProviderConfiguration` | `keycloak.platform-mesh.io/v1alpha1` `IdentityProviderConfiguration` |
| APIExports `core.platform-mesh.io` (invites), `system.platform-mesh.io` (identityproviderconfigurations) | APIExport `keycloak.platform-mesh.io` |
| WorkspaceType `security` (initializer `root:security`) runs the identity setup | WorkspaceType `keycloak` (initializer/terminator `root:keycloak`) |
| OIDC client secrets labelled `core.platform-mesh.io/{idp-name,client-name}` | additionally `keycloak.platform-mesh.io/{idp-name,client-name}` |

Both groups are served during the transition. pm-keycloak-operator reconciles
the deprecated kinds through the old exports when the `LegacyAPIs`
feature gate is enabled. It is enabled by default.

## What platform-mesh-operator does on its own

The `keycloak-apigroup-split` migration step runs on every reconcile of
the `PlatformMesh` resources once v26.11 is deployed:

1. Binds `keycloak.platform-mesh.io` into every existing organization
   and account workspace. `root:platform-mesh-system` and `root:orgs`
   get theirs from the kcp manifests.
2. Adds the `keycloak.platform-mesh.io/{idp-name,client-name}` labels to the
   OIDC client secrets of every deprecated `IdentityProviderConfiguration`,
   so the keycloak export's secrets claim selects them. The legacy labels stay.
3. Adds `root:keycloak` to `spec.terminators` of every organization
   LogicalCluster, so deleting an organization created before v26.11 still
   removes its realm and clients.

The portal still reads `identityproviderconfigurations.core.platform-mesh.io`
(`welcome` and the organization realms), so a deprecated twin is kept next to
every `keycloak.platform-mesh.io` object the platform owns: platform-mesh-operator
applies one for `welcome`, pm-keycloak-operator's initializer creates one per
organization. The twins are created marked `keycloak.platform-mesh.io/migrated=true`,
only the keycloak object's reconciler touches the realm and mirrors its status
into the twin. They go once the portal reads the new group.

This is skipped entirely when the `feature-disable-keycloak` toggle is set.

## Moving an object to the new group

Deprecated objects keep working. To move one create the `keycloak.platform-mesh.io`
object next to it and remove the deprecated one afterwards:

1. Create the `keycloak.platform-mesh.io/v1alpha1` object with the same
   name and spec in the same workspace. The validating webhook accepts
   it although the realm already exists, because a deprecated object of
   that name is present in the same workspace.
2. pm-keycloak-operator adopts the realm: it copies `status.managedClients`
   from the deprecated object (no new Keycloak clients, secrets unchanged) and
   annotates the deprecated object `keycloak.platform-mesh.io/migrated=true`.
   From then on the deprecated object is no longer reconciled, its status
   mirrors the new object's.
3. Wait until the new object reports `Ready`.
4. Delete the deprecated object. A migrated object is released without
   touching Keycloak.

Deleting the deprecated object *before* its successor exists deletes the
realm.

`Invite`s need no hand-over: create the new one, delete the old one in
any order - the Keycloak user is only created once.

With GitOps, commit the new object first and the removal of the old one
in a later change.

## Operator checklist

1. Upgrade to v26.11. `pm-keycloak-operator` must be enabled in the profile
   (it is in the shipped `kind-profile.yaml` / `production-profile.yaml`),
   security-operator no longer takes `keycloak`/`idp` values.

2. Check the migration step ran:

   ```shell
   kubectl -n platform-mesh-system logs deploy/platform-mesh-operator | grep keycloak-apigroup-split
   ```

3. Move your own `IdentityProviderConfiguration`/`Invite` manifests to
   `keycloak.platform-mesh.io/v1alpha1` as described above. Requests against
   the deprecated kinds return a deprecation `Warning`. iam-service and the
   `welcome` realm applied by platform-mesh-operator already use the new
   group, the portal does not yet.

4. Once no deprecated objects are left anywhere:

   ```shell
   KUBECONFIG=<kcp admin kubeconfig>
   kubectl get identityproviderconfigurations.core.platform-mesh.io --server=<kcp>/clusters/root:orgs
   kubectl get identityproviderconfigurations.core.platform-mesh.io --server=<kcp>/clusters/root:platform-mesh-system
   ```

   pm-keycloak-operator can run with `--feature-gates=LegacyAPIs=false`,
   which also drops the legacy webhook rule. The deprecated kinds are
   removed from the `core` and `system` exports in a later release.

## Rollback

Deprecated objects that were not moved are unaffected by a downgrade. A moved
object has its deprecated twin marked `keycloak.platform-mesh.io/migrated=true`;
remove that annotation to hand the realm back to the deprecated object.
