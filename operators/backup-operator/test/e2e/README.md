# backup-operator e2e tests

The tests run against a real Platform Mesh in kind: root shard and `triton` shard, etcd-druid with an S3 store (LocalStack or MinIO), and the backup-operator built from this repository. S3 runs as docker container `backup-s3` in the kind network, so it stays when the cluster is deleted. The tests reach it on `http://localhost:4566`.

## Setup

```shell
./setup/setup.sh            # S3 is LocalStack
S3=minio ./setup/setup.sh   # S3 is MinIO
```

It takes about 30 minutes the first time. It needs about 25 GB free disk in Docker, Postgres and Keycloak break when the disk is full. Running it again on the same cluster only updates the operator. For a new cluster it empties the druid store bucket.

## Run

```shell
task test-e2e
```

The restores and one of the backups run with the `backup-operator` binary, built by the test. The test does this:

1. Creates a workspace on each shard with a marker ConfigMap, an org Account, and a provider kubeconfig with a `Kubeconfig` object.
2. Saves the uids, the logical cluster names and the CA fingerprints. Takes one backup with a `Backup` object and one with the backup command.
3. Loses data and restores it, one case after the other:
   - only etcd (data and druid store), plus a workspace made after the backup, which must be gone after the restore
   - only the kcp secrets (cert-manager issues new keys before the restore)
   - only the kcp Certificates and Issuers, with kcp-operator stopped
   - everything, from the archive of the backup command
4. After each restore it checks:
   - all Deployments and StatefulSets are ready and PlatformMesh is Ready
   - the same workspaces and org, with the same uid, and the markers
   - the same CA fingerprints, and the admin and provider kubeconfigs from before the data loss still work
   - a new workspace on each shard and a new org become Ready
5. Negative cases: a backup fails when an etcd is not ready (object and command), a restore with a wrong key or of a missing backup fails without stopping anything.

## New cluster

```shell
task test-e2e-fresh
```

It runs [fresh.sh](fresh.sh): the same data and a backup on the current cluster, then `kind delete cluster`, `setup.sh` for a new install, and a restore into it with the same checks as above. The old admin and provider kubeconfigs must work in the new cluster. It takes about one hour. Use `S3=minio task test-e2e-fresh` when the cluster runs with MinIO.
