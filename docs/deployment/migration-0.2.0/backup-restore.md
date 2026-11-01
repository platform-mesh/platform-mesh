# Backup and Restore Guide

This guide covers backing up and restoring the platform-mesh data stores: **etcd** (kcp), **OpenFGA**, and **Keycloak**. These procedures are applicable during version upgrades and migration scenarios. Data restoration is not always required.

## Prerequisites

- `fga` CLI installed
- `kubectl` configured with access to the target cluster
- etcd-backup-restore Docker image from [gardener/etcd-backup-restore](https://github.com/gardener/etcd-backup-restore):

  ```text
  europe-docker.pkg.dev/gardener-project/public/gardener/etcdbrctl:v0.41.0
  ```

## Backup

> **Note:** Ensure you have a valid backup before attempting any restore operation.

### Preparation

Create the backup directories:

```shell
mkdir -p backup/etcd
mkdir -p backup/fga/postgres
mkdir -p backup/keycloak/postgres
```

Load the etcd-backup-restore image into the cluster and port-forward the OpenFGA service:

```shell
kind load docker-image europe-docker.pkg.dev/gardener-project/public/gardener/etcdbrctl:v0.41.0 --name platform-mesh
kubectl -n platform-mesh-system port-forward svc/openfga 3000 8080 8081
```

### Backup OpenFGA

Backs up the OpenFGA PostgreSQL database and exports the store using the FGA CLI.

```shell
local-setup/scripts/fga_backup.sh
```

### Backup etcd (kcp)

Creates a full etcd snapshot.

```shell
local-setup/scripts/etcd_backup.sh
```

### Backup Keycloak

Backs up the Keycloak PostgreSQL database and the database credentials secret.

```shell
local-setup/scripts/keycloak_backup.sh
```

## Restore

### Restore OpenFGA

Deletes the existing pod, PVC, and PV, waits for the StatefulSet to recreate them with a clean volume, and restores the PostgreSQL dump.

```shell
local-setup/scripts/openfga_postgres_restore.sh
```

### Restore etcd (kcp)

**Step 1** -- Scale down the etcd StatefulSet:

```shell
kubectl -n platform-mesh-system scale statefulset etcd-kcp --replicas=0
```

**Step 2** -- Create a temporary restore pod with the etcd PVC mounted:

```shell
kubectl -n platform-mesh-system apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: etcd-restore
  namespace: platform-mesh-system
spec:
  containers:
  - name: restore
    image: europe-docker.pkg.dev/gardener-project/public/gardener/etcdbrctl:v0.41.0
    command: ["sleep", "3600"]
    volumeMounts:
    - name: etcd-data
      mountPath: /var/etcd/data
  volumes:
  - name: etcd-data
    persistentVolumeClaim:
      claimName: etcd-kcp-etcd-kcp-0
EOF
```

**Step 3** -- Copy the snapshot into the restore pod (run from the host):

```shell
kubectl -n platform-mesh-system exec -ti etcd-restore -- mkdir -p /root/tmp/etcd-backup/v2
kubectl -n platform-mesh-system cp \
  backup/etcd/Full-00000000-00016935-1770191336.gz \
  etcd-restore:/root/tmp/etcd-backup/v2/Full-00000000-00016935-1770191336.gz.final
```

**Step 4** -- Restore the data (run inside the restore pod):

```shell
kubectl -n platform-mesh-system exec -it etcd-restore -- sh
```

Once inside the pod, run:

```shell
./etcdbrctl restore \
  --storage-provider="Local" \
  --store-container="/tmp/etcd-backup" \
  --data-dir="/tmp/etcd-data" \
  --restoration-temp-snapshots-dir="/tmp/restoration.tmp"
```

**Step 5** -- Fix permissions on the restored data (still inside the pod):

```shell
chown -R 65532:65532 /var/etcd/data/new.etcd/member/
```

### Restore Keycloak

Similar to OpenFGA, this deletes the existing pod, PVC, and PV, waits for recreation, restores the database secret, and then restores the PostgreSQL dump using the password from the secret.

```shell
local-setup/scripts/keycloak_postgres_restore.sh
```
