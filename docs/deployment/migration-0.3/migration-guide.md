# Migration Guide: 0.2.0 → 0.3.0

## Strategy

This is a **hybrid migration**. KCP state (etcd) is wiped and rebuilt from scratch because in-place schema upgrades are unreliable. Keycloak and OpenFGA databases are preserved in place — their data is migrated without a full delete/restore cycle. Only the OpenFGA tuples need ID fixups to match the new KCP cluster IDs.

| Component | Migration approach |
|---|---|
| KCP | Wipe etcd, fresh install, recreate workspaces, restore user resources |
| Keycloak | In-place — restore PostgreSQL secret, restart |
| OpenFGA | In-place — migrate tuples with updated cluster IDs |

---

## 1. Version matrix

| Component | 0.2.0 | 0.3.0 | Notes |
|---|---|---|---|
| OCM component | `0.2.0` | `0.3.0-build.1249` | drives all sub-component versions |
| OCM install method | Kustomization + GitRepository | HelmRelease (`ocm-k8s-toolkit`) | breaking change |
| platform-mesh-operator | 0.18.x | 0.19.x | |
| security-operator | 0.23.x | 0.29.x | new FGA `bind`/`bind_inherited` types |
| account-operator | — | 0.1.x | new; manages account lifecycle and FGA tuples |
| OpenFGA | 0.2.51 | 0.2.54 | |
| Keycloak | 25.x | 25.3.0 | |
| KCP | v0.29/v0.30 | 097afc12c | custom image; new `system.platform-mesh.io` APIExport |
| portal | 0.10.x | 0.10.145 | CRD gateway URL path changed |
| marketplace-ui | disabled | enabled | |
| init-agent | — | 0.2.0 | new component |

---

## 2. Pre-migration (on 0.2 cluster)

### 2.1 Create test data

Resources not covered by `task test:portal-e2e`:

- Account `testaccount` under `default` org
- HttpBin `test1` in `:root:orgs:default:testaccount` (namespace `default`)

### 2.2 Backup

```shell
local-setup/scripts/keycloak_backup.sh
local-setup/scripts/keycloak_export_realms.sh
local-setup/scripts/fga_backup.sh
local-setup/scripts/etcd_backup.sh
docs/migration-0.3/export-kcp-resources.sh local-setup/backup/kcp/0.2-userdata
docs/migration-0.3/export-kcp-resources.sh local-setup/backup/kcp-exports/pre-migration
```

---

## 3. Install 0.3

### 3.1 Tear down 0.2

```shell
kind delete cluster --name platform-mesh
```

If preserving the Kind cluster:

```shell
kubectl -n platform-mesh-system delete PlatformMesh platform-mesh
kubectl delete components platform-mesh
kubectl delete repositories platform-mesh
kubectl delete PlatformMeshOperator platform-mesh-operator
kubectl delete ResourceGraphDefinition platform-mesh-operator
kubectl delete Resource --all
kubectl delete OCIRepository --all
kubectl delete Etcd etcd-kcp -n platform-mesh-system
kubectl delete HelmRelease --all
kubectl delete secret --all -n platform-mesh-system
kubectl delete kustomizations ocm-k8s-toolkit
kubectl delete persistentvolumeclaims etcd-kcp-etcd-kcp-0 -n platform-mesh-system
```

### 3.2 Deploy 0.3

```shell
git checkout feat/migration-0.3
task local-setup -- --example-data
```

Wait for readiness:

```shell
kubectl get helmrelease -A
kubectl get platformmesh -n platform-mesh-system
```

---

## 4. Restore Keycloak

```shell
kubectl apply -f local-setup/backup/keycloak/postgres/keycloak-postgresql-keycloak.yaml
local-setup/scripts/keycloak_restore.sh
kubectl rollout restart statefulset/keycloak -n platform-mesh-system
kubectl rollout status statefulset/keycloak -n platform-mesh-system
```

Delete stale Keycloak clients (named after old GUIDs) from the `default` realm in the admin console.

---

## 5. Restore KCP and OpenFGA

### 5.1 Disable the IPC webhook

```shell
KUBECONFIG=.secret/kcp/admin.kubeconfig kubectl \
  --server="https://localhost:8443/clusters/root:platform-mesh-system" \
  delete ValidatingWebhookConfiguration \
  identityproviderconfiguration-validator.webhooks.core.platform-mesh.io
```

### 5.2 Recreate organizations and accounts

Create organizations and accounts via the browser (e.g., `default` org and its sub-accounts). The operator provisions workspace infrastructure — this must complete before restoring data.

### 5.3 Migrate OpenFGA tuples

Stop operators that reconcile tuples:

```shell
kubectl scale deployment/security-operator -n platform-mesh-system --replicas=0
kubectl scale deployment/account-operator -n platform-mesh-system --replicas=0
```

Run the migration (replaces old KCP cluster IDs with current ones, writes to OpenFGA):

```shell
docs/migration-0.3/migrate-openfga-tuples.sh <backup-dir> [output-dir]
```

Restore operators:

```shell
kubectl scale deployment/account-operator -n platform-mesh-system --replicas=1
kubectl scale deployment/security-operator -n platform-mesh-system --replicas=1
```

### 5.4 Restore HttpBin resources

```shell
docs/migration-0.3/restore-kcp-resources.sh <export-dir>
```

### 5.5 Re-enable the IPC webhook

The operator recreates it on next reconciliation, or restart the security-operator.

---

## 6. Post-restore

```shell
kubectl rollout restart deployment/rebac-authz-webhook -n platform-mesh-system
kubectl rollout status deployment/rebac-authz-webhook -n platform-mesh-system
kubectl -n platform-mesh-system rollout restart deployment/kubernetes-graphql-gateway-listener
kubectl -n platform-mesh-system rollout status deployment/kubernetes-graphql-gateway-listener
```

---

## 7. Verification

```shell
task test:portal-e2e
```
