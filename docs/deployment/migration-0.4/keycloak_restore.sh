#!/usr/bin/env bash
set -euo pipefail

BACKUP_DIR="${1:-${BACKUP_DIR:-local-setup/backup/keycloak/postgres}}"

# Use the most recent backup unless BACKUP_FILE is set explicitly
BACKUP_FILE="${BACKUP_FILE:-$(ls -t "$BACKUP_DIR"/backup-*.sql | head -1)}"

if [[ -z "$BACKUP_FILE" || ! -f "$BACKUP_FILE" ]]; then
  echo "Error: no backup file found in $BACKUP_DIR" >&2
  echo "Set BACKUP_FILE=<path> to specify one explicitly." >&2
  exit 1
fi

echo "=== Keycloak Restore (0.4 / cnpg) ==="
echo "Backup file: $BACKUP_FILE"

echo "Step 1: Waiting for cnpg primary pod..."
kubectl wait pod/platform-mesh-pg-1 \
  -n platform-mesh-system \
  --for=condition=Ready \
  --timeout=120s

echo "Step 2: Terminating active connections to keycloak database..."
kubectl exec -n platform-mesh-system platform-mesh-pg-1 -- \
  psql -U postgres -d postgres -c \
    "SELECT pg_terminate_backend(pid) FROM pg_stat_activity \
      WHERE datname = 'keycloak' AND pid <> pg_backend_pid();"

echo "Step 3: Dropping and recreating database..."
kubectl exec -n platform-mesh-system platform-mesh-pg-1 -- \
  dropdb -U postgres keycloak
kubectl exec -n platform-mesh-system platform-mesh-pg-1 -- \
  createdb -U postgres -O keycloak keycloak

echo "Step 4: Restoring from backup..."
kubectl exec -i -n platform-mesh-system platform-mesh-pg-1 -- \
  psql -U postgres keycloak \
  < "$BACKUP_FILE"

echo "Step 5: Restarting Keycloak..."
kubectl rollout restart statefulset/keycloak -n platform-mesh-system
kubectl rollout status statefulset/keycloak -n platform-mesh-system --timeout=120s

echo "=== Keycloak Restore Complete ==="
echo "Backup restored from: $BACKUP_FILE"
