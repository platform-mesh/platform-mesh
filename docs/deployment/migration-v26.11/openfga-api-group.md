# Migrate Stores, AuthorizationModels and APIExportPolicies to openfga.platform-mesh.io

v26.11 renames security-operator to pm-openfga-operator, rebac-authz-webhook
to pm-openfga-authz-webhook, and gives the OpenFGA kinds their own API group:

| before (deprecated)                                          | after                                                          |
| ------------------------------------------------------------ | -------------------------------------------------------------- |
| `core.platform-mesh.io/v1alpha1` `Store`                     | `openfga.platform-mesh.io/v1alpha1` `Store`                    |
| `core.platform-mesh.io/v1alpha1` `AuthorizationModel`        | `openfga.platform-mesh.io/v1alpha1` `AuthorizationModel`       |
| `core.platform-mesh.io/v1alpha1` `APIExportPolicy`           | `openfga.platform-mesh.io/v1alpha1` `APIExportPolicy`          |
| APIExports `fga.platform-mesh.io` (stores), `provider-fga.platform-mesh.io` (authorizationmodels), `system.platform-mesh.io` (apiexportpolicies) | APIExports `openfga.platform-mesh.io` (stores, apiexportpolicies), `provider-openfga.platform-mesh.io` (authorizationmodels) |
| WorkspaceType `security` (initializer/terminator `root:security`) | WorkspaceType `openfga` (initializer/terminator `root:openfga`) |
| Helm charts `security-operator`, `rebac-authz-webhook`       | `pm-openfga-operator`, `pm-openfga-authz-webhook`              |
| Secrets `security-operator-kubeconfig`, `security-{initializer,terminator}-kubeconfig`, `rebac-authz-webhook-kubeconfig`, `rebac-authz-webhook-cert` | `pm-openfga-operator-kubeconfig`, `openfga-{initializer,terminator}-kubeconfig`, `pm-openfga-authz-webhook-kubeconfig`, `pm-openfga-authz-webhook-cert` |

Both groups are served during the transition. pm-openfga-operator reconciles
the deprecated kinds through the old exports while its `LegacyAPIs` feature
gate is on (default `true`); requests against them get a deprecation
`Warning`. pm-openfga-authz-webhook reads Stores from `openfga.platform-mesh.io`
only.

## What platform-mesh-operator does on its own

The `openfga-apigroup-split` migration step runs on every reconcile of the
`PlatformMesh` once v26.11 is deployed. It changes the environment, never
the deprecated objects themselves:

1. Replaces `root:security` with `root:openfga` in `spec.terminators` and
   `status.terminators` of every organization and account LogicalCluster.
   Nothing serves the old terminator anymore; without this, deleting a
   workspace created before v26.11 would hang.
2. Binds `provider-openfga.platform-mesh.io` into every existing provider
   workspace, so the model generator writes AuthorizationModels there.
   `root:orgs` gets `openfga.platform-mesh.io` from the kcp manifests.
3. Renders the kcp authorization webhook kubeconfig and PKI under the new
   names. The old `rebac-authz-webhook-*` Certificates and Secrets stay
   behind; delete them once the new webhook serves.

Skipped entirely when the `feature-disable-openfga` toggle is set.

## What pm-openfga-operator does on its own

Stores and generated AuthorizationModels are platform-owned, so the operator
moves them itself once the workspace binds the new export:

- For every deprecated `Store` in `root:orgs` it creates the
  `openfga.platform-mesh.io` Store of the same name and spec. The new Store
  adopts `status.storeId`, `status.authorizationModelId`,
  `status.managedTuples` and the `authzmodel.core.platform-mesh.io/*`
  dependency finalizers, and annotates the deprecated Store
  `openfga.platform-mesh.io/migrated=true`. No second OpenFGA store is
  created. The deprecated Store is deleted together with the organization
  (terminator) and released without touching OpenFGA.
- The model generator writes its AuthorizationModels in the group the
  provider workspace bound (`provider-openfga` preferred). Once a new model
  adopted its deprecated twin, the generator deletes the twin.

## Moving an APIExportPolicy to the new group

`APIExportPolicy` objects belong to providers. Deprecated ones keep working.
To move one create the `openfga.platform-mesh.io` object next to it and
remove the deprecated one afterwards:

1. Create the `openfga.platform-mesh.io/v1alpha1` object with the same name
   and spec in the same workspace (`root:orgs`).
2. pm-openfga-operator adopts it: `status.managedAllowExpressions` is copied
   and the deprecated object is annotated
   `openfga.platform-mesh.io/migrated=true`. From then on the deprecated
   object is no longer reconciled.
3. Wait until the new object reports `Ready`.
4. Delete the deprecated object. A migrated object is released without
   removing the bind tuples.

Deleting the deprecated object *before* its successor exists removes the
bind tuples. With GitOps, commit the new object first and the removal of the
old one in a later change.

## Operator checklist

1. Upgrade to v26.11 with `pm-openfga-operator` and `pm-openfga-authz-webhook`
   enabled in the profile (they are in the shipped `kind-profile.yaml` /
   `production-profile.yaml`); remove the `security-operator` and
   `rebac-authz-webhook` entries. Values keep their keys.

2. Check the migration step ran:

   ```shell
   kubectl -n platform-mesh-system logs deploy/platform-mesh-operator | grep openfga-apigroup-split
   ```

3. Check every organization Store moved:

   ```shell
   KUBECONFIG=<kcp admin kubeconfig>
   kubectl get stores.openfga.platform-mesh.io --server=<kcp>/clusters/root:orgs
   kubectl get stores.core.platform-mesh.io --server=<kcp>/clusters/root:orgs \
     -o custom-columns='NAME:.metadata.name,MIGRATED:.metadata.annotations.openfga\.platform-mesh\.io/migrated'
   ```

4. Move your `APIExportPolicy` manifests to `openfga.platform-mesh.io/v1alpha1`
   as described above.

5. Remove the orphaned webhook PKI:

   ```shell
   kubectl -n platform-mesh-system delete certificate rebac-authz-webhook-cert rebac-authz-webhook-ca
   kubectl -n platform-mesh-system delete issuer rebac-authz-webhook-ca-issuer rebac-authz-webhook-selfsigned-issuer
   kubectl -n platform-mesh-system delete secret rebac-authz-webhook-cert rebac-authz-webhook-ca rebac-authz-webhook-kubeconfig
   kubectl -n platform-mesh-system delete secret security-operator-kubeconfig security-initializer-kubeconfig security-terminator-kubeconfig
   ```

6. Once no deprecated objects are left anywhere:

   ```shell
   kubectl get apiexportpolicies.core.platform-mesh.io --server=<kcp>/clusters/root:orgs
   kubectl get authorizationmodels.core.platform-mesh.io --server=<kcp>/clusters/root:providers:<provider>
   ```

   pm-openfga-operator can run with `--feature-gates=LegacyAPIs=false`
   (chart value `featureGates.LegacyAPIs: false`). The deprecated kinds, the
   `fga`/`provider-fga` exports and the `apiexportpolicies` resource of the
   `system` export are removed in a later release, together with the
   `security` WorkspaceType.

## Rollback

Deprecated objects that were not moved are unaffected by a downgrade. A moved
object has its deprecated twin marked `openfga.platform-mesh.io/migrated=true`;
remove that annotation to hand the OpenFGA state back to the deprecated
object. Terminators must be switched back to `root:security` by hand.
