# backup-operator e2e tests

The tests run against a Platform Mesh in kind with a root shard and a `triton` shard, etcd-druid with an S3 store (LocalStack or MinIO), and the backup-operator. They stop kcp and restore etcd, so they do not run next to the other suites: `go test ./e2e/...` skips them, they only run with `task tests:e2e:backup`.

## Setup

```shell
task tests:e2e:backup:setup            # S3 is LocalStack
S3=minio task tests:e2e:backup:setup   # S3 is MinIO
```

It runs [setup/setup.sh](setup/setup.sh). It creates the kind cluster `platform-mesh` with the backup-operator built from this repository. S3 runs as docker container `backup-s3` in the kind network, so it stays when the cluster is deleted. The tests reach it on `http://localhost:4566`, the cluster on `http://backup-s3:4566`. `BACKUP_S3_ENDPOINT` and `BACKUP_S3_CLUSTER_ENDPOINT` change them.

It takes about 30 minutes the first time and needs about 25 GB free disk in Docker. Running it again on the same cluster only updates the operator. For a new cluster it empties the druid store bucket.

## Run

```shell
task tests:e2e:backup
```

Like the other e2e suites, the tests find Platform Mesh with `KUBECONFIG`, `KIND_CONTEXT` and `ADMIN_KUBECONFIG` (see [tests/framework/pm](../../framework/pm/pm.go)). The task sets them for the kind cluster when they are not set. The restores and one of the backups run with the `backup-operator` binary, built by the test. The test does this:

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

A restore into a new cluster is not tested here yet. [local-kind.md](../../../operators/backup-operator/docs/development/local-kind.md) shows it by hand.
