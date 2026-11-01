# Migration guide 0.4 → 0.5

## Breaking changes in 0.5

No structural breaking changes: Helm release names, kcp workspace layout, resource types, Keycloak realm/client configuration, and OpenFGA authorization model schema are identical between 0.4 and 0.5.

The following behavioral changes require attention during migration:

- **Stale authorization model IDs** — the security-operator creates new model versions on upgrade. After a DB restore from a 0.4 backup, the IDs written into `Store` CR status will not exist in the restored DB. See step 5a.
- **IDP client IDs** — `kubectl` and `{orgname}` client UUIDs in `AccountInfo` and `IdentityProviderConfiguration` resources are fresh after each install and will not match values from the prior install. See step 5.1.

## Investigation notes

> These notes use local-setup tooling and are intended for testing the migration on a local environment.

## Migration procedure

### 1. Backup (for comparison)

```shell
# 0.4 backup
BACKUPDIR=backup/0.4
local-setup/scripts/keycloak_backup.sh $BACKUPDIR/keycloak/postgres
local-setup/scripts/keycloak_export_realms.sh $BACKUPDIR/keycloak/realms
local-setup/scripts/fga_backup.sh $BACKUPDIR/openfga
local-setup/scripts/etcd_backup.sh $BACKUPDIR/etcd
KUBECONFIG_KCP=.secret/kcp/admin.kubeconfig local-setup/scripts/export-resources.sh $BACKUPDIR
```

### 2. Remove 0.4-only resources

```shell
kubectl delete platformmeshes platform-mesh -n platform-mesh-system
kubectl delete platformmeshoperators --all
kubectl delete resourcegraphdefinitions platform-mesh-operator
kubectl delete crd platformmeshoperators.kro.run

kubectl delete resource --all -n platform-mesh-system
kubectl delete component platform-mesh -n platform-mesh-system
kubectl delete repositories platform-mesh -n platform-mesh-system
kubectl delete helmreleases --all -n platform-mesh-system
```

### 3. Install 0.5

Install 0.5 following the standard install procedure for your environment.

> For local development environments using the kustomize-based local-setup:
> ```shell
> git checkout 0.5.0
> kubectl apply -k local-setup/kustomize/base/ocm-k8s-toolkit
> kubectl apply -k local-setup/kustomize/base/kro
> kubectl apply -k local-setup/kustomize/components/ocm   # set SEMVER to 0.5.0 manually
> kubectl apply -k local-setup/kustomize/base/rgd
> kubectl apply -k local-setup/kustomize/components/platform-mesh-operator
> kubectl apply -k local-setup/kustomize/overlays/platform-mesh-resource
> kubectl get platformmesh -A -w
> ```

### 4. Restore stateful data

```shell
# Keycloak — restores into cnpg cluster (platform-mesh-pg-1)
BACKUP_DIR=backup/0.4/keycloak/postgres docs/migration-0.4/keycloak_restore.sh

# OpenFGA — restores into openfga-postgres-0
BACKUP_DIR=backup/0.4/openfga/postgres docs/migration-0.4/openfga_restore.sh
```

#### 4.1 fix client secret for IdP resources in kcp

Some `IdentityProviderConfiguration` resources in kcp's `:root:orgs` workspace might have an invalid state:

```yaml
- lastTransitionTime: "2026-09-13T12:06:29Z"
  message: 'failed to create or update realm: failed to create realm: Post "https://portal.localhost:8443/keycloak/admin/realms": oauth2: "unauthorized_client" "Invalid client or Invalid client credentials"'
  observedGeneration: 1
  reason: Error
  status: "False"
  type: IdentityProviderConfiguration
- lastTransitionTime: "2026-09-13T12:06:29Z"
  message: one or more subroutines encountered an error
  observedGeneration: 1
  reason: Error
  status: "False"
  type: Ready
```

Is is due to the client secrets having different values than the ones in keycloak. To fix this, update the secret in kcp named `portal-client-secret-test-${orgname}-${orgname}` to contain the same secret as the corresponding keycloak application in the appropriate realm.

### 5. Verify

Check that the portal is functional and the test data created in step 1 is intact.

### 6. Back up 0.5

```shell
BACKUPDIR=backup/0.5
scripts/keycloak_backup.sh $BACKUPDIR/keycloak/postgres
scripts/keycloak_export_realms.sh $BACKUPDIR/keycloak/realms
scripts/fga_backup.sh $BACKUPDIR/openfga
scripts/etcd_backup.sh $BACKUPDIR/etcd
KUBECONFIG_KCP=<path-to-kcp-kubeconfig> scripts/export-resources.sh $BACKUPDIR
```
