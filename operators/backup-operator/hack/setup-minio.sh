#!/usr/bin/env bash
set -euo pipefail

NS=platform-mesh-system

# MinIO
ACCESS_KEY=minioadmin
SECRET_KEY=minioadmin
HOST_PATH=/var/lib/minio-etcd-backups
ENDPOINT=http://minio.${NS}.svc.cluster.local:9000

# etcd
ETCD_SECRET=etcd-backup-s3
ETCD_BUCKET=etcd-backups

# CNPG
CNPG_SECRET=cnpg-backup-s3
CNPG_BUCKET=cnpg-backups
CNPG_CLUSTER=platform-mesh-pg

# Velero
VELERO_NS=platform-mesh-velero
VELERO_SECRET=velero-backup-s3
VELERO_BUCKET=velero-backups

VELERO_CREDENTIALS_FILE="$(mktemp)"
trap 'rm -f "$VELERO_CREDENTIALS_FILE"' EXIT

cat > "$VELERO_CREDENTIALS_FILE" <<EOF
[default]
aws_access_key_id=${ACCESS_KEY}
aws_secret_access_key=${SECRET_KEY}
EOF

kubectl apply -n "$NS" -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: minio
spec:
  replicas: 1
  selector:
    matchLabels:
      app: minio
  template:
    metadata:
      labels:
        app: minio
    spec:
      containers:
      - name: minio
        image: minio/minio:latest
        args:
        - server
        - /data
        - --console-address
        - ":9001"
        env:
        - name: MINIO_ROOT_USER
          value: ${ACCESS_KEY}
        - name: MINIO_ROOT_PASSWORD
          value: ${SECRET_KEY}
        ports:
        - name: api
          containerPort: 9000
        - name: console
          containerPort: 9001
        volumeMounts:
        - name: data
          mountPath: /data
      volumes:
      - name: data
        hostPath:
          path: ${HOST_PATH}
          type: DirectoryOrCreate
---
apiVersion: v1
kind: Service
metadata:
  name: minio
spec:
  selector:
    app: minio
  ports:
  - name: api
    port: 9000
    targetPort: api
  - name: console
    port: 9001
    targetPort: console
EOF

kubectl rollout status deployment/minio -n "$NS"

# Ensure Velero namespace exists before creating the Velero credentials secret.
# The backup-operator also reconciles this namespace, but the script needs it now.
kubectl create namespace "$VELERO_NS" \
  --dry-run=client -o yaml | kubectl apply -f -

# etcd secret
kubectl create secret generic "$ETCD_SECRET" \
  -n "$NS" \
  --from-literal=accessKeyID="$ACCESS_KEY" \
  --from-literal=secretAccessKey="$SECRET_KEY" \
  --from-literal=region="us-east-1" \
  --from-literal=s3ForcePathStyle=true \
  --dry-run=client -o yaml | kubectl apply -f -

# CNPG secret
kubectl create secret generic "$CNPG_SECRET" \
  -n "$NS" \
  --from-literal=ACCESS_KEY_ID="$ACCESS_KEY" \
  --from-literal=SECRET_ACCESS_KEY="$SECRET_KEY" \
  --dry-run=client -o yaml | kubectl apply -f -

# Velero secret.
# Velero expects a key named "cloud" containing an AWS credentials file.
kubectl create secret generic "$VELERO_SECRET" \
  -n "$VELERO_NS" \
  --from-file=cloud="$VELERO_CREDENTIALS_FILE" \
  --dry-run=client -o yaml | kubectl apply -f -

# Create buckets
kubectl run minio-mc \
  -n "$NS" \
  --rm -i --restart=Never \
  --image=minio/mc:latest \
  --command -- sh -c "
    mc alias set local $ENDPOINT $ACCESS_KEY $SECRET_KEY &&
    mc mb --ignore-existing local/$ETCD_BUCKET &&
    mc mb --ignore-existing local/$CNPG_BUCKET &&
    mc mb --ignore-existing local/$VELERO_BUCKET
  "

# Patch etcd backup config
for ETCD in etcd-kcp etcd-kcp-nereus etcd-kcp-triton; do
  kubectl patch etcd "$ETCD" \
    -n "$NS" \
    --type merge \
    -p "{
      \"spec\": {
        \"backup\": {
          \"store\": {
            \"provider\": \"aws\",
            \"container\": \"$ETCD_BUCKET\",
            \"prefix\": \"$ETCD\",
            \"endpointOverride\": \"$ENDPOINT\",
            \"secretRef\": {
              \"name\": \"$ETCD_SECRET\"
            }
          }
        }
      }
    }"
done

# Patch CNPG backup config
kubectl patch cluster "$CNPG_CLUSTER" \
  -n "$NS" \
  --type merge \
  -p "{
    \"spec\": {
      \"backup\": {
        \"barmanObjectStore\": {
          \"destinationPath\": \"s3://$CNPG_BUCKET/$CNPG_CLUSTER\",
          \"endpointURL\": \"$ENDPOINT\",
          \"s3Credentials\": {
            \"accessKeyId\": {
              \"name\": \"$CNPG_SECRET\",
              \"key\": \"ACCESS_KEY_ID\"
            },
            \"secretAccessKey\": {
              \"name\": \"$CNPG_SECRET\",
              \"key\": \"SECRET_ACCESS_KEY\"
            }
          },
          \"wal\": {
            \"compression\": \"gzip\"
          },
          \"data\": {
            \"compression\": \"gzip\"
          }
        }
      }
    }
  }"

echo "MinIO backup prerequisites configured using hostPath: ${HOST_PATH}"
echo "MinIO endpoint: ${ENDPOINT}"
echo "etcd bucket: ${ETCD_BUCKET}"
echo "CNPG bucket: ${CNPG_BUCKET}"
echo "Velero bucket: ${VELERO_BUCKET}"
echo "Velero secret: ${VELERO_NS}/${VELERO_SECRET}"
echo "CNPG cluster patched: ${NS}/${CNPG_CLUSTER}"
