# Local kind cluster

How to run Platform Mesh with the backup-operator in kind, and how to test a backup and a restore into a new cluster by hand. Run all commands in the repository root.

## What you need

- `docker`, `kind`, `kubectl` 1.31 or newer, `helm`, `jq`, `openssl`, `go`, `python3`
- about 25 GB free disk in Docker

## Create the cluster

```sh
operators/backup-operator/test/e2e/setup/setup.sh            # S3 is LocalStack
S3=minio operators/backup-operator/test/e2e/setup/setup.sh   # S3 is MinIO
```

It creates the kind cluster `platform-mesh` with Platform Mesh (root and `triton` shard), etcd-druid, and the backup-operator built from this repository. S3 runs in the docker container `backup-s3`, on `http://localhost:4566` from your machine and `http://backup-s3:4566` in the cluster, with the access key `backup-e2e` and the secret `backup-e2e-secret`. The container stays when the cluster is deleted, so a backup survives a new cluster. To change from one S3 to the other, run `docker rm -f backup-s3` first. MinIO keeps its data in the docker volume `backup-s3-data`.

It takes about 30 minutes the first time. Run it again on the same cluster to update the operator.

## Run the e2e tests

```sh
cd operators/backup-operator
task test-e2e         # backups and restores on the current cluster
task test-e2e-fresh   # backup, delete the cluster, install it again and restore, about one hour
```

See [test/e2e/README.md](../../test/e2e/README.md) for what they check.

## Backup and restore by hand

This is what `task test-e2e-fresh` does, step by step. A kubeconfig minted with a `Kubeconfig` object before the backup must work after the restore into a new cluster.

### 1. Helpers

```sh
export NS=platform-mesh-system
export WORK=/tmp/backup-local
export KCP_URL=https://kcp.api.portal.localhost:8443
mkdir -p $WORK
(cd operators/backup-operator && go build -o $WORK/backup-operator .)

# kubectl for kcp with the admin kubeconfig from the old cluster: kcp <workspace path> <args>
kcp() { local p=$1; shift; kubectl --kubeconfig $WORK/admin.kubeconfig --server "$KCP_URL/clusters/$p" "$@"; }

# backup-operator backup|restore against the S3 in backup-s3
bo() {
  AWS_ACCESS_KEY_ID=backup-e2e AWS_SECRET_ACCESS_KEY=backup-e2e-secret $WORK/backup-operator "$@" --context kind-platform-mesh \
    --name local --bucket platform-backups --prefix "$(cat $WORK/prefix)" --endpoint http://localhost:4566 \
    --path-style --etcd-store-endpoint http://localhost:4566 --key-file $WORK/backup.key
}
```

### 2. Kubeconfigs and test data

Save the admin kubeconfig, and mint a provider kubeconfig with a `Kubeconfig` object like a platform does for its operators:

```sh
kubectl -n $NS get secret kubeconfig-kcp-admin -o jsonpath='{.data.kubeconfig}' | base64 -d > $WORK/admin.kubeconfig
kubectl apply -f - <<EOF
apiVersion: operator.kcp.io/v1alpha1
kind: Kubeconfig
metadata: {name: bk-provider, namespace: $NS}
spec:
  username: bk-provider
  groups: [system:kcp:admin]
  validity: 8766h
  secretRef: {name: bk-provider-kubeconfig}
  target: {frontProxyRef: {name: frontproxy}}
EOF
kubectl -n $NS wait --for=create secret/bk-provider-kubeconfig --timeout=5m
kubectl -n $NS get secret bk-provider-kubeconfig -o jsonpath='{.data.kubeconfig}' | base64 -d > $WORK/provider.kubeconfig
```

A workspace on each shard with a marker ConfigMap:

```sh
for s in root triton; do
  kcp root apply -f - <<EOF
apiVersion: tenancy.kcp.io/v1alpha1
kind: Workspace
metadata: {name: bk-$s}
spec:
  type: {name: universal, path: root}
  location: {selector: {matchLabels: {name: $s}}}
EOF
  kcp root wait --for=jsonpath='{.status.phase}'=Ready workspace/bk-$s --timeout=5m
  kcp root:bk-$s create configmap bk-marker -n default --from-literal=value=before-backup
done
kcp root get workspaces bk-root bk-triton -o custom-columns=NAME:.metadata.name,UID:.metadata.uid --no-headers | tee $WORK/before.txt
```

### 3. Backup

The key must be exactly 32 bytes, so no newline at the end. Keep the key, the archive cannot be read without it.

```sh
echo local-$(date +%Y%m%d-%H%M%S) > $WORK/prefix
printf %s "$(openssl rand -hex 16)" > $WORK/backup.key
bo backup
```

The output ends with the archive name, for example `archive local-.../platform-mesh-system/local.tar`.

### 4. Lose the cluster and install it again

```sh
kind delete cluster --name platform-mesh
operators/backup-operator/test/e2e/setup/setup.sh    # with S3=minio when you use MinIO
```

The new cluster has new CAs, so the old kubeconfigs do not work yet. Both commands fail with a certificate error:

```sh
kcp root get workspaces
kubectl --kubeconfig $WORK/provider.kubeconfig --server "$KCP_URL/clusters/root" get workspaces
```

### 5. Restore

```sh
bo restore
```

The last line is `restore of [secrets certificates etcd] completed`.

### 6. Check

```sh
kubectl -n $NS wait --for=condition=Available deploy --all --timeout=20m
kubectl -n $NS wait --for=condition=Ready platformmeshes/platform-mesh --timeout=20m

# same workspaces with the same uid, with the old admin kubeconfig
kcp root get workspaces bk-root bk-triton -o custom-columns=NAME:.metadata.name,UID:.metadata.uid --no-headers | diff $WORK/before.txt - && echo OK
kcp root:bk-root get configmap bk-marker -n default -o jsonpath='{.data.value}{"\n"}'     # before-backup
kcp root:bk-triton get configmap bk-marker -n default -o jsonpath='{.data.value}{"\n"}'   # before-backup

# the provider kubeconfig from before the backup works
kubectl --kubeconfig $WORK/provider.kubeconfig --server "$KCP_URL/clusters/root" get workspaces

# kcp-operator wrote the provider kubeconfig secret again, with the old client certificate
cert() { kubectl config view --kubeconfig $1 --raw -o jsonpath='{.users[0].user.client-certificate-data}'; }
kubectl -n $NS get secret bk-provider-kubeconfig -o jsonpath='{.data.kubeconfig}' | base64 -d > $WORK/provider-after.kubeconfig
[ "$(cert $WORK/provider.kubeconfig)" = "$(cert $WORK/provider-after.kubeconfig)" ] && echo OK

# new workspaces still work
kcp root apply -f - <<EOF
apiVersion: tenancy.kcp.io/v1alpha1
kind: Workspace
metadata: {name: bk-new}
spec:
  type: {name: universal, path: root}
EOF
kcp root wait --for=jsonpath='{.status.phase}'=Ready workspace/bk-new --timeout=5m
```

### Clean up

```sh
kcp root delete workspace bk-root bk-triton bk-new
kubectl -n $NS delete kubeconfigs.operator.kcp.io bk-provider
```

To remove everything, also run `kind delete cluster --name platform-mesh`, `docker rm -f backup-s3` and `docker volume rm backup-s3-data`.
