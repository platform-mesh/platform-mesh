# Platform Mesh - backup-operator

## Description

The backup-operator backs up the kcp of Platform Mesh into one archive in S3, and restores it. It runs in the runtime cluster, next to kcp-operator and etcd-druid, and does not talk to kcp itself.

A backup has three parts:

- **etcd**: one full snapshot of every etcd-druid `Etcd` in the namespace (root shard, other shards, cache server).
- **secrets**: the secrets of the kcp certificates: CAs, service account keys and leaf certificates.
- **certificates**: the cert-manager `Certificate` and `Issuer` objects of kcp.

Secrets and certificates are the Certificates owned by kcp-operator (`RootShard`, `Shard`, `CacheServer`, `FrontProxy`), plus every Certificate issued by a kcp CA. This includes the client certificates of `Kubeconfig` objects. Kubeconfig secrets are not in the backup, kcp-operator and platform-mesh-operator create them again.

## Archive

One backup is one object `<prefix>/<namespace>/<name>.tar` in an S3 compatible bucket. It has:

- `pki.json.enc`: the secrets, Certificates, Issuers and kcp-operator Kubeconfigs
- `etcd/<etcd>/<snapshot>.enc`: one druid snapshot for each etcd
- `manifest.json`: the topology, and size and sha256 of each entry. It is the last entry.

Every entry except the manifest is encrypted with AES-256-GCM and a 32 byte key, in chunks of 64 KiB. The etcd snapshots have all kcp data, so they are encrypted too. The archive is streamed to S3, the object only exists when the upload is complete.

## Backup

There are two ways, both run the same code.

With the operator: create a `BackupStorage` and a `Backup`. Group `backup.platform-mesh.io/v1alpha1`, the objects live in the namespace of kcp.

- `BackupStorage`: bucket, prefix, endpoint, credentials secret (`accessKeyID`, `secretAccessKey`) and encryption key secret (`key`, 32 bytes).
- `Backup`: creating it takes one backup. It can not be changed later. `status.archive` is the key of the archive.

```yaml
apiVersion: backup.platform-mesh.io/v1alpha1
kind: Backup
metadata:
  name: before-upgrade
  namespace: platform-mesh-system
spec:
  storageRef:
    name: default
```

With the command, from a machine with a kubeconfig of the runtime cluster:

```shell
export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=...
backup-operator backup --name before-upgrade \
  --bucket platform-backups --prefix pm --endpoint https://s3.example.com --path-style \
  --key-file backup.key
```

`--etcd-store-endpoint` replaces the endpoint of the druid stores, when the one in the `Etcd` only works inside the cluster.

## Restore

A restore is disaster recovery, so it is a command and not an object. It needs a Platform Mesh with the same topology: the same cluster, or a new install with the same RootShard, Shards, CacheServer, FrontProxy and Etcd names and store prefixes.

```shell
backup-operator restore --name before-upgrade \
  --bucket platform-backups --prefix pm --endpoint https://s3.example.com --path-style \
  --key-file backup.key
```

`--only secrets,certificates,etcd` restores some parts, default is all. The steps:

1. Download the archive and check every entry with the key. Check that the namespace, the kcp topology and the etcd store prefixes are the same as in the backup. If not, the restore fails and nothing is changed.
2. Stop platform-mesh-operator, kcp-operator, the kcp Deployments and the kcp clients (every Deployment that mounts a `*-kubeconfig` secret, like the portal and the security-operator). The replicas are kept in the annotation `backup.platform-mesh.io/replicas`.
3. Write the secrets back. The CertificateRequests of the kcp Certificates are deleted first, otherwise cert-manager issues a new key at once. The secrets must stay unchanged for 10 seconds, else they are written again.
4. Create the missing Certificates, Issuers and Kubeconfigs. kcp-operator writes the kubeconfig secrets again from the restored client certificates.
5. For each etcd: stop it and delete its volumes, move the old objects of the druid store to `before-restore-<time>/<prefix>/`, and put only the snapshot from the backup in the store. When the etcd starts, druid restores it from that snapshot.
6. Start the kcp Deployments and kcp-operator, and wait until the RootShard and the Shards are running.
7. Start the kcp clients and platform-mesh-operator.

The etcd must be stopped before the druid store is prepared: druid uploads a last delta snapshot when it stops. When a restore fails, run it again: it continues with the replicas from the annotations.

A kubeconfig from before the backup works after the restore, also in a new install, because the kcp CAs and the data in etcd are the ones from the backup.

## Requirements and limits

- etcd-druid with an S3 store on every `Etcd`, each with its own prefix. No prefix may start with the prefix of another `Etcd` in the same bucket, like `etcd-kcp` and `etcd-kcp-triton`: druid lists the store without a trailing slash and mixes their snapshots. Backup and restore check this.
- A new install must start with an empty druid store, else druid restores the old data before the restore runs.
- OpenFGA and Keycloak data (Postgres) are not in the backup.
- Only one restore may run at a time.

## Testing

- `task test` runs the unit tests.
- `task test-e2e` runs the kind e2e tests, see [test/e2e/README.md](test/e2e/README.md).
- `task test-e2e-fresh` backs up, installs a new kind cluster and restores there.
- [docs/development/local-kind.md](docs/development/local-kind.md) shows the local kind setup and the same backup and restore by hand.

## Contributing

Please refer to the [CONTRIBUTING.md](../../CONTRIBUTING.md) file in this repository for instructions on how to contribute to Platform Mesh.

## Code of Conduct

Please refer to our [Code of Conduct](https://github.com/platform-mesh/.github/blob/main/CODE_OF_CONDUCT.md) for information on the expected conduct for contributing to Platform Mesh.

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
