# Migration guide 0.3 → 0.4

## Investigation notes

> These notes use local-setup tooling and are intended for testing the migration on a local environment.

### Backup (for comparison)

```shell
# 0.3 backup
BACKUPDIR=backup/0.3
local-setup/scripts/keycloak_backup.sh $BACKUPDIR/keycloak/postgres
local-setup/scripts/keycloak_export_realms.sh $BACKUPDIR/keycloak/realms
local-setup/scripts/fga_backup.sh $BACKUPDIR/openfga
local-setup/scripts/etcd_backup.sh $BACKUPDIR/etcd
KUBECONFIG_KCP=.secret/kcp/admin.kubeconfig local-setup/scripts/export-resources.sh $BACKUPDIR
```

### Key differences in 0.4

**kcp**
- kcp sharding enabled
- New APIExport `providers.platform-mesh.io` with two new resource types:
  - `providers.providers.platform-mesh.io`
  - `providerpermissions.providers.platform-mesh.io`
- New kcp workspace: `root:providers:system`

**Infrastructure**
- Databases provisioned via cnpg operator (not embedded charts)
- Keycloak deployed via keycloak-operator; pod is `keycloak-0`; Keycloak DB on cnpg cluster `platform-mesh-pg`
- OCM resources moved from `default` namespace to `platform-mesh-system`

**Credentials**
- Keycloak admin secret: `keycloak-admin` (keys: `username`, `password`) — previously `keycloak-postgresql-keycloak`
- OpenFGA postgres secret: `openfga-postgres` (key: `postgres-password`)
- Keycloak DB credentials: `keycloak-db-credentials` (keys: `username`, `password`)

**What does NOT change**
- Keycloak realm and client configuration is identical between 0.3 and 0.4 (only instance UUIDs differ)
- OpenFGA authorization models and tuple structure are identical; only kcp shard UIDs embedded in object IDs differ (instance-specific, not portable across installs)

---

## Migration procedure

### 1. Install 0.3 and verify

Install 0.3 and verify the portal is functional.

### 2. Back up 0.3

The backup scripts are in `scripts/` of this repo. Run them against the target cluster:

```shell
BACKUPDIR=backup/0.3
scripts/keycloak_backup.sh $BACKUPDIR/keycloak/postgres
scripts/keycloak_export_realms.sh $BACKUPDIR/keycloak/realms
scripts/fga_backup.sh $BACKUPDIR/openfga
scripts/etcd_backup.sh $BACKUPDIR/etcd
KUBECONFIG_KCP=<path-to-kcp-kubeconfig> scripts/export-resources.sh $BACKUPDIR
```

### 3. Remove 0.3-only resources

```shell
kubectl delete platformmeshes platform-mesh -n platform-mesh-system
kubectl delete platformmeshoperators --all
kubectl delete resourcegraphdefinitions platform-mesh-operator
kubectl delete crd platformmeshoperators.kro.run

kubectl delete resource --all
kubectl delete component platform-mesh
kubectl delete repositories platform-mesh
kubectl delete helmreleases platform-mesh-operator-components
kubectl delete helmreleases platform-mesh-operator-infra-components # force-delete "traefik"
kubectl delete ocirepositories --all -n default
```

### 4. Install 0.4

Install 0.4 following the standard install procedure for your environment.

> For local development environments using the kustomize-based local-setup:
> ```shell
> git checkout 0.4.0
> kubectl apply -k local-setup/kustomize/base/ocm-k8s-toolkit
> kubectl apply -k local-setup/kustomize/base/kro
> kubectl apply -k local-setup/kustomize/components/ocm   # set SEMVER to 0.4.0 manually
> kubectl apply -k local-setup/kustomize/base/rgd
> kubectl apply -k local-setup/kustomize/components/platform-mesh-operator
> kubectl apply -k local-setup/kustomize/overlays/platform-mesh-resource
> kubectl get platformmesh -A -w
> ```

### 5. Restore stateful data

```shell
# Keycloak — restores into cnpg cluster (platform-mesh-pg-1)
BACKUP_DIR=backup/0.3/keycloak/postgres docs/migration-0.4/keycloak_restore.sh

# OpenFGA — restores into openfga-postgres-0
BACKUP_DIR=backup/0.3/openfga/postgres docs/migration-0.4/openfga_restore.sh
```

### 5a. Fix stale authorization model IDs (if needed)

If the security-operator logs show errors like:
```
unable to read authorization model error="Authorization Model '<id>' not found"
```
this means the security-operator reconciled the `Store` CRs before the OpenFGA restore ran and wrote
fresh model IDs into the status that no longer exist in the restored DB. Fix by clearing the stale IDs
so the operator re-reads the latest model from the restored DB on next reconcile:

```shell
# Run for each org Store CR (replace with actual org names)
KUBECONFIG_KCP=<path-to-kcp-kubeconfig> kubectl patch stores.core.platform-mesh.io <org-name> \
  --subresource=status --type=merge \
  -p '{"status":{"authorizationModelId":""}}'
```

Then restart the account-operator to bring Account resources back to Ready:

```shell
kubectl rollout restart deployment/account-operator -n platform-mesh-system
```

### 6. Verify

Check that the portal is functional and the test data created in step 1 is intact.

### 7. Back up 0.4

```shell
BACKUPDIR=backup/0.4
scripts/keycloak_backup.sh $BACKUPDIR/keycloak/postgres
scripts/keycloak_export_realms.sh $BACKUPDIR/keycloak/realms
scripts/fga_backup.sh $BACKUPDIR/openfga
scripts/etcd_backup.sh $BACKUPDIR/etcd
KUBECONFIG_KCP=<path-to-kcp-kubeconfig> scripts/export-resources.sh $BACKUPDIR
```
