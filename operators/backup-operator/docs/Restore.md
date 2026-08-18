# Restore PoC

This document describes the end-to-end workflow for validating the `backup-operator` restore functionality using a previously created Platform Mesh backup.

## Prerequisites

- A working Platform Mesh development environment
- A backup created using the [Backup PoC](Backup.md)
- The local `minio-etcd-backups` directory containing the backup artefacts
- `kind`, `kubectl`, `task`, `make`, and `yq` installed
- Docker installed

---

## 1. Create a Platform Mesh cluster

Create a fresh Platform Mesh cluster without loading the example data:

```bash
task local-setup:concurrent
```

This cluster will be used as the target for the restore operation.

---

## 2. Enable automatic reconciliation in etcd-druid

Edit the etcd-druid operator ConfigMap:

```bash
kubectl edit cm etcd-druid-operator-config -n etcd-druid-system
```

Ensure the following configuration is enabled:

```yaml
enableEtcdSpecAutoReconcile: true
```

Then roll out the etcd-druid deployment so that a new operator pod is created with the updated configuration:

```bash
kubectl rollout restart deployment etcd-druid-operator -n etcd-druid-system
```

Wait until the new operator pod is running before continuing.

---

## 3. Deploy MinIO

Deploy MinIO using the provided setup script:

```bash
./hack/setup-minio.sh
```

---

## 4. Verify the MinIO directory layout

Ensure that the expected backup directories exist inside the MinIO pod:

```bash
ls -al /data/*
```

Expected layout:

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

The following directories should be present:

- `/data/cnpg-backups`
- `/data/etcd-backups`
- `/data/velero-backups`

---

## 5. Copy the backup into MinIO

Copy the previously generated backup from the local filesystem into the MinIO container:

```bash
docker cp ./minio-etcd-backups/. $(kind-docker-id):/var/lib/minio-etcd-backups/
```

Verify that the backup artefacts are present in the MinIO pod before proceeding.

---

## 6. Deploy the backup operator

Deploy the backup operator:

```bash
make deploy
```

---

## 7. Create a PlatformRestore resource

Apply the `PlatformRestore` custom resource:

```bash
kubectl apply -f config/samples/backup_v1alpha1_platformrestore-velero.yaml
```

---

## 8. Watch the restore status

Watch the `PlatformRestore` resource until the restore completes:

```bash
kubectl get platformrestores.backup.platform-mesh.io \
  platformrestore-velero-sample \
  -w
```

The restore progresses through several phases.

Expected output:

```text
NAME                            PHASE                     READY
platformrestore-velero-sample   QuiescingPlatform         False
platformrestore-velero-sample   QuiescingPlatform         True
platformrestore-velero-sample   RestoringEtcd             False
platformrestore-velero-sample   RestoringEtcd             True
platformrestore-velero-sample   RestoringCNPG             False
platformrestore-velero-sample   RestoringCNPG             True
platformrestore-velero-sample   RestoringVelero           False
platformrestore-velero-sample   RestoringVelero           False
platformrestore-velero-sample   RestoringVelero           True
platformrestore-velero-sample   RestartingControlPlane    False
platformrestore-velero-sample   RestartingControlPlane    False
platformrestore-velero-sample   RestartingControlPlane    False
platformrestore-velero-sample   RestartingControlPlane    True
platformrestore-velero-sample   ValidatingIdentity        False
platformrestore-velero-sample   Succeeded                 True
```

The final state must be:

```text
platformrestore-velero-sample   Succeeded   True
```

The restore workflow consists of the following major phases:

1. **QuiescingPlatform** — Prepare the platform for restoration.
2. **RestoringEtcd** — Restore the etcd data and snapshots.
3. **RestoringCNPG** — Restore the CloudNativePG database backup.
4. **RestoringVelero** — Restore Velero-managed resources.
5. **RestartingControlPlane** — Restart the Platform Mesh control plane.
6. **ValidatingIdentity** — Validate the restored identity and platform state.
7. **Succeeded** — Restore completed successfully.

---

## 9. Verify the restored platform

After the `PlatformRestore` reaches:

```text
PHASE      READY
Succeeded  True
```

verify that the Platform Mesh platform is operational.

---

## 10. Verify the restored identity

Using the Platform Mesh UI, verify that the identity data from the original backup has been restored.

You should be able to log in using the same:

- **Admin account**
- **Organization**
- **User**

that were created before the backup was taken.

Successful login and visibility of the restored organization and user confirm that the identity-related data was successfully restored.
