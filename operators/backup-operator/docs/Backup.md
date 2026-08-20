# Backup PoC

This document describes the end-to-end workflow for validating the `backup-operator` Proof of Concept (PoC).

## Prerequisites

- A working Platform Mesh development environment
- `kind`, `kubectl`, `task`, `make`, and `yq` installed
- Docker installed

---

# 1. Create a Platform Mesh cluster

Create a new Platform Mesh cluster and populate it with the example data.

```bash
task local-setup:concurrent:example-data
```

---

# 2. Create test data

Using the Platform Mesh UI:

1. Create an **Admin** account.
2. Create an **Organization**.
3. Create a **User** within that organization.

These resources will later be validated after the restore process.

---

# 3. Enable automatic reconciliation in etcd-druid

Edit the etcd-druid operator configuration.

```bash
kubectl edit cm etcd-druid-operator-config -n etcd-druid-system
```

Ensure the following setting is enabled:

```yaml
enableEtcdSpecAutoReconcile: true
```

After saving the ConfigMap, restart the operator so that it picks up the new configuration.

For example:

```bash
kubectl rollout restart deployment etcd-druid-operator -n etcd-druid-system
```

Wait until the new operator pod is running before continuing.

---

# 4. Deploy MinIO

Deploy the local MinIO instance.

```bash
./hack/setup-minio.sh
```

---

# 5. Verify the MinIO directory layout

Verify that the expected backup directories have been created inside the MinIO pod.

```bash
ls -al /data/*
```

Expected output:

```text
/data/cnpg-backups:
total 8
drwxr-xr-x 2 root root 4096 Aug 18 20:17 .
drwxr-xr-x 6 root root 4096 Aug 18 20:17 ..

/data/etcd-backups:
total 8
drwxr-xr-x 2 root root 4096 Aug 18 20:18 .
drwxr-xr-x 6 root root 4096 Aug 18 20:17 ..

/data/velero-backups:
total 8
drwxr-xr-x 2 root root 4096 Aug 18 20:17 .
drwxr-xr-x 6 root root 4096 Aug 18 20:17 ..
```

---

# 6. Deploy the backup operator

Deploy the backup operator.

```bash
make deploy
```

---

# 7. Create a PlatformBackup resource

Apply the sample `PlatformBackup` custom resource.

```bash
kubectl apply -f config/samples/backup_v1alpha1_platformbackup-velero.yaml
```

---

# 8. Verify that all backup artefacts completed successfully

Inspect the backup status.

```bash
kubectl get platformbackups.backup.platform-mesh.io \
    platformbackup-velero-sample \
    -o yaml | yq
```

A successful backup should contain artefacts for all enabled components.

```yaml
apiVersion: backup.platform-mesh.io/v1alpha1
kind: PlatformBackup

spec:
  components:
    cnpg:
      enabled: true
    etcd:
      enabled: true
    velero:
      enabled: true

  storage:
    s3:
      bucket: velero-backups
      credentialsRef:
        name: velero-backup-s3
      endpoint: http://minio.platform-mesh-system.svc.cluster.local:9000
      region: us-east-1

status:
  artefacts:
    cnpg:
      backups:
        platform-mesh-pg: platformbackup-velero-sample-platform-mesh-pg-backup

    etcd:
      shards:
        etcd-kcp:
          snapshotKey: "1375"
          snapshotTime: "2026-08-18T19:51:13Z"

        etcd-kcp-nereus:
          snapshotKey: "801"
          snapshotTime: "2026-08-18T19:51:13Z"

        etcd-kcp-triton:
          snapshotKey: "1021"
          snapshotTime: "2026-08-18T19:51:13Z"

    velero:
      backupName: platformbackup-velero-sample-platform-mesh
```

Verify that:

- CNPG backup completed successfully.
- All etcd shards have snapshot keys.
- Velero backup has been created.
- No component reports an error.

---

# 9. Copy the backup data locally

Copy the backup artefacts from the MinIO container to your local machine.

```bash
docker cp $(kind-container-id):/var/lib/minio-etcd-backups/. ./minio-etcd-backups/
```

---

# 10. Remove MinIO metadata

Before using the backup for restore testing, remove the MinIO internal metadata.

```bash
rm -rf ./minio-etcd-backups/.minio.sys
```

The resulting directory can now be reused during the restore PoC.