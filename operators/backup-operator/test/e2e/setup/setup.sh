#!/usr/bin/env bash

# Copyright The Platform Mesh Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Kind cluster with Platform Mesh (root and triton shard), LocalStack or MinIO S3 as druid store,
# and the backup-operator built from this repository. Safe to run again on the same cluster.
# S3 runs as docker container backup-s3 in the kind network, so it survives a new cluster.
set -euo pipefail

CLUSTER=${CLUSTER:-platform-mesh}
DIR=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$DIR/../../../../.." && pwd)
NS=platform-mesh-system
BASE_DOMAIN=portal.localhost
PM_VERSION=${PM_VERSION:-0.6.0-build.6}
PMO_CHART=0.61.0
PREREQ_CHART=0.2.2
IMAGE=ghcr.io/platform-mesh/platform-mesh/backup-operator:e2e
S3=${S3:-localstack}
# the e2e tests use the same keys, MinIO needs a secret of at least 8 characters
S3_ACCESS_KEY=backup-e2e
S3_SECRET_KEY=backup-e2e-secret

log() { echo "$(date +%H:%M:%S) $*"; }

new_cluster=false
if kind get clusters | grep -qx "$CLUSTER"; then
  log "kind cluster $CLUSTER exists, reuse it"
else
  new_cluster=true
  log "create kind cluster"
  kind create cluster --name "$CLUSTER" --image kindest/node:v1.35.1 --config=- <<'EOF'
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
networking:
  apiServerAddress: "127.0.0.1"
  kubeProxyMode: iptables
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry]
      config_path = "/etc/containerd/certs.d"
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 31000
        hostPort: 8443
        protocol: TCP
        listenAddress: "127.0.0.1"
EOF
fi
kubectl config use-context "kind-$CLUSTER" >/dev/null

# Optional pull-through caches (registry:2 containers in the kind network), they save a lot of time.
node=$CLUSTER-control-plane
for pair in ghcr.io:proxy-ghcr quay.io:proxy-quay registry.k8s.io:proxy-k8s-io; do
  reg=${pair%%:*}; proxy=${pair##*:}
  docker inspect "$proxy" >/dev/null 2>&1 || continue
  docker exec "$node" mkdir -p "/etc/containerd/certs.d/$reg"
  printf 'server = "https://%s"\n\n[host."http://%s:5000"]\n  capabilities = ["pull", "resolve"]\n' "$reg" "$proxy" |
    docker exec -i "$node" tee "/etc/containerd/certs.d/$reg/hosts.toml" >/dev/null
done

kubectl create namespace $NS --dry-run=client -o yaml | kubectl apply -f -

case $S3 in
  localstack) s3_image=docker.io/localstack/localstack:4.4 ;;
  # MinIO does not publish free images anymore, this one is built from the MinIO source
  minio) s3_image=cgr.dev/chainguard/minio@sha256:db3f583fa49121942168bd6383a773b2cde3384c9a99bf7193560b9a0f3a28fd ;;
  *) echo "S3 must be localstack or minio, got $S3" >&2; exit 1 ;;
esac
log "start s3 ($S3) on http://localhost:4566 and in the kind network on http://backup-s3:4566"
if running=$(docker inspect -f '{{.Config.Image}}' backup-s3 2>/dev/null); then
  if [ "$running" != "$s3_image" ]; then
    echo "backup-s3 runs $running, not $s3_image. Remove it first: docker rm -f backup-s3" >&2
    exit 1
  fi
elif [ "$S3" = minio ]; then
  docker run -d --name backup-s3 --network kind -p 127.0.0.1:4566:4566 -v backup-s3-data:/data \
    -e MINIO_ROOT_USER=$S3_ACCESS_KEY -e MINIO_ROOT_PASSWORD=$S3_SECRET_KEY $s3_image server /data --address :4566 >/dev/null
else
  docker run -d --name backup-s3 --network kind -p 127.0.0.1:4566:4566 \
    -e SERVICES=s3 -e PERSISTENCE=0 $s3_image >/dev/null
fi
aws() {
  docker run --rm --network kind -e AWS_ACCESS_KEY_ID=$S3_ACCESS_KEY -e AWS_SECRET_ACCESS_KEY=$S3_SECRET_KEY -e AWS_DEFAULT_REGION=us-east-1 \
    docker.io/amazon/aws-cli:2.27.0 --endpoint-url http://backup-s3:4566 "$@"
}
until aws s3 ls >/dev/null 2>&1; do sleep 2; done
for b in etcd-backups platform-backups; do
  aws s3 ls "s3://$b" >/dev/null 2>&1 || aws s3 mb "s3://$b"
done
if $new_cluster; then
  # druid restores a new etcd from the store, a new cluster must not start with old data
  aws s3 rm --recursive --quiet s3://etcd-backups/
fi
# older versions of this script ran LocalStack in the cluster
kubectl delete namespace objstore --ignore-not-found --wait=false

# druid reads the endpoint from spec.backup.store.endpointOverride, see patch-profile.py
kubectl -n $NS create secret generic etcd-backup-s3 \
  --from-literal=accessKeyID=$S3_ACCESS_KEY \
  --from-literal=secretAccessKey=$S3_SECRET_KEY \
  --from-literal=region=us-east-1 \
  --from-literal=s3ForcePathStyle=true \
  --dry-run=client -o yaml | kubectl apply -f -

log "install prerequisites"
prereq=(oci://ghcr.io/platform-mesh/helm-charts/platform-mesh-prerequisites --version $PREREQ_CHART --namespace $NS)
helm upgrade --install platform-mesh-prerequisites "${prereq[@]}" \
  --set keycloak-operator.watchNamespaces=$NS --wait --timeout 15m
helm upgrade platform-mesh-prerequisites "${prereq[@]}" --reset-values --wait --timeout 15m \
  --set keycloak-operator.watchNamespaces=$NS \
  --set traefik.enabled=true \
  --set traefik.service.type=NodePort \
  --set traefik.service.spec.type=NodePort \
  --set traefik.service.spec.clusterIP=10.96.188.4 \
  --set traefik.ports.websecure.exposedPort=8443 \
  --set traefik.ports.websecure.nodePort=31000

log "patch kind profile: triton shard and druid store"
work=$(mktemp -d)
helm pull oci://ghcr.io/platform-mesh/helm-charts/platform-mesh-operator --version $PMO_CHART --untar --untardir "$work"
python3 "$DIR/patch-profile.py" "$work/platform-mesh-operator/files/kind-profile.yaml"

log "install platform-mesh-operator"
traefik_ip=$(kubectl -n $NS get svc platform-mesh-prerequisites-traefik -o jsonpath='{.spec.clusterIP}')
helm upgrade --install platform-mesh-operator "$work/platform-mesh-operator" --namespace $NS \
  --set ocm.enabled=true \
  --set ocm.profile=kind \
  --set gateway.baseDomain=$BASE_DOMAIN \
  --set gateway.port=8443 \
  --set gateway.traefikClusterIP="$traefik_ip" \
  --set kcp.frontProxyPort=8443 \
  --set certificates.selfSigned.enabled=true \
  --set-string ocm.version="$PM_VERSION" \
  --wait --timeout 15m
rm -rf "$work"

log "build and install backup-operator"
docker build -q -t $IMAGE -f "$ROOT/operators/backup-operator/Dockerfile" "$ROOT"
kind load docker-image $IMAGE --name "$CLUSTER"
helm dependency build "$ROOT/charts/backup-operator" >/dev/null
# helm installs crds/ only once, apply them for runs on an existing cluster
kubectl apply --server-side -f "$ROOT/charts/backup-operator/crds/"
helm upgrade --install backup-operator "$ROOT/charts/backup-operator" --namespace $NS \
  --set image.tag=e2e --set image.pullPolicy=Never --set log.level=debug --wait --timeout 5m
kubectl -n $NS rollout restart deploy/backup-operator
kubectl -n $NS rollout status deploy/backup-operator --timeout=5m

# druid changes the etcd StatefulSets only when asked, for example after the store endpoint changed
kubectl -n $NS annotate etcd --all --overwrite gardener.cloud/operation=reconcile
sleep 10
kubectl -n $NS get sts -o name | grep '/etcd-' | xargs -r -n1 kubectl -n $NS rollout status --timeout=10m
# kcp clients do not recover when the etcds restart: Deployments with a *-kubeconfig secret that kcp-operator does not own
kubectl -n $NS get deploy -o json | jq -r '.items[]
  | select(all(.metadata.ownerReferences[]?; (.apiVersion|startswith("operator.kcp.io/")) | not))
  | select(any(.spec.template.spec.volumes[]?; .secret.secretName // "" | endswith("-kubeconfig"))) | .metadata.name' |
  xargs -r kubectl -n $NS rollout restart deploy

log "wait for platform mesh"
kubectl -n $NS wait --for=condition=Ready certificate/platform-mesh-domain --timeout=10m
kubectl -n $NS wait --for=condition=Ready platformmeshes/platform-mesh --timeout=60m
log "platform mesh ready"
