#!/usr/bin/env bash
set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKUP_DIR="${BACKUP_DIR:-$SCRIPT_DIR/../../backup/openfga-migration}"
NAMESPACE="${NAMESPACE:-platform-mesh-system}"
OPENFGA_SELECTOR='app.kubernetes.io/name=openfga,app.kubernetes.io/instance=openfga'

usage() {
  echo "Usage: $0 backup [backup-dir]" >&2
  echo "       $0 restore <backup-file.dump>" >&2
  echo "Set the profile's OpenFGA replicaCount to 0 and autoscaling.enabled to false first." >&2
  echo "Keep the old datastore and postgresql.enabled=true until restore succeeds." >&2
  exit 1
}

fail() {
  echo "Error: $*" >&2
  exit 1
}

k() { kubectl -n "$NAMESPACE" "$@"; }

# A manual scale-down can be undone by Flux/PMO. Require the reconciled Helm
# values to express maintenance mode, and never automatically restart writers.
require_maintenance() {
  local replicas autoscaling bundled pods
  replicas=$(k get helmrelease openfga -o jsonpath='{.spec.values.replicaCount}')
  autoscaling=$(k get helmrelease openfga -o jsonpath='{.spec.values.autoscaling.enabled}')
  bundled=$(k get helmrelease openfga -o jsonpath='{.spec.values.postgresql.enabled}')
  [[ "$replicas" == 0 && "$autoscaling" == false && "$bundled" == true ]] ||
    fail "set replicaCount=0, autoscaling.enabled=false and postgresql.enabled=true in the OpenFGA profile; wait for PMO/Flux to reconcile."
  replicas=$(k get deployment openfga -o jsonpath='{.spec.replicas}')
  [[ "$replicas" == 0 ]] || fail "OpenFGA deployment has not reconciled to zero replicas."
  k wait pods -l "$OPENFGA_SELECTOR" --for=delete --timeout=120s
  pods=$(k get pods -l "$OPENFGA_SELECTOR" -o name)
  [[ -z "$pods" ]] || fail "OpenFGA pods are still present."
}

backup() {
  local backup_dir="${1:-$BACKUP_DIR}" partial backup_file pgpassword
  require_maintenance
  k wait pod/openfga-postgres-0 --for=condition=Ready --timeout=120s
  pgpassword=$(k get secret openfga-postgres -o jsonpath='{.data.postgres-password}' | base64 -d)
  [[ -n "$pgpassword" ]] || fail "the source postgres password is empty."

  mkdir -p "$backup_dir"
  partial=$(mktemp "$backup_dir/backup-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX.dump.partial")
  # Send the password as data on stdin, never interpolate it into shell code or
  # command arguments. A failed dump remains .partial and has no checksum.
  printf '%s\0' "$pgpassword" | k exec -i openfga-postgres-0 -- bash -c '
    IFS= read -r -d "" PGPASSWORD
    export PGPASSWORD
    exec pg_dump -U postgres -d postgres --schema=public --format=custom --no-owner --no-acl
  ' > "$partial"
  [[ -s "$partial" ]] || fail "pg_dump produced an empty archive."
  require_maintenance
  backup_file="${partial%.partial}"
  mv "$partial" "$backup_file"
  sha256sum "$backup_file" | cut -d ' ' -f 1 > "$backup_file.sha256"
  echo "Backup complete: $backup_file"
  echo "Keep OpenFGA stopped and retain both the archive and its .sha256 file."
  echo "Restore into CNPG before changing the datastore or disabling the old PostgreSQL."
}

primary_sql() {
  k exec "$PRIMARY" -c postgres -- psql -X -U postgres -d openfga \
    --set=ON_ERROR_STOP=1 --tuples-only --no-align -c "$1"
}

restore() {
  local backup_file="${1:-}" expected actual username writable owner empty
  [[ -n "$backup_file" && -s "$backup_file" && "$backup_file" == *.dump ]] ||
    fail "pass an explicit, non-empty .dump archive produced by the backup command."
  [[ -s "$backup_file.sha256" ]] || fail "missing backup checksum: $backup_file.sha256"
  expected=$(cat "$backup_file.sha256")
  actual=$(sha256sum "$backup_file" | cut -d ' ' -f 1)
  [[ "$expected" =~ ^[a-f0-9]{64}$ && "$actual" == "$expected" ]] || fail "backup checksum mismatch."

  require_maintenance
  k wait database/openfga-db --for=jsonpath='{.status.applied}'=true --timeout=300s
  PRIMARY=$(k get cluster.postgresql.cnpg.io platform-mesh-pg -o jsonpath='{.status.currentPrimary}')
  [[ -n "$PRIMARY" ]] || fail "CNPG has not reported a current primary."
  k wait "pod/$PRIMARY" --for=condition=Ready --timeout=180s
  writable=$(primary_sql "SELECT NOT pg_is_in_recovery() AND current_setting('transaction_read_only') = 'off';")
  [[ "$writable" == t ]] || fail "CNPG primary changed or is read-only; retry after it stabilizes."
  username=$(k get secret cnpg-openfga-user -o jsonpath='{.data.username}' | base64 -d)
  [[ "$username" == openfga ]] || fail "this procedure requires the CNPG role openfga."
  owner=$(primary_sql "SELECT pg_get_userbyid(datdba) = 'openfga' FROM pg_database WHERE datname = current_database();")
  [[ "$owner" == t ]] || fail "the destination database must be owned by openfga."
  empty=$(primary_sql "SELECT NOT EXISTS (SELECT FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public');")
  [[ "$empty" == t ]] || fail "destination public schema is not empty; refusing to overwrite existing data."
  k exec -i "$PRIMARY" -c postgres -- pg_restore --list < "$backup_file" > /dev/null

  # The whole archive is one transaction. Failure rolls it back; restoring as
  # openfga gives that role ownership of tables, sequences and other objects.
  # --clean replaces the empty public schema inside that same transaction.
  # No superuser-owned tables or grants to PUBLIC are needed.
  require_maintenance
  k exec -i "$PRIMARY" -c postgres -- pg_restore -U postgres -d openfga \
    --single-transaction --exit-on-error --clean --if-exists --no-owner --no-acl --role=openfga \
    < "$backup_file"
  echo "Restore complete on $PRIMARY. OpenFGA remains stopped."
  echo "Verify the restored data, then switch the profile to CNPG and resume OpenFGA."
  echo "Keep the old PostgreSQL and backup until application verification succeeds."
}

CMD="${1:-}"
shift || true
[[ $# -le 1 ]] || usage
case "$CMD" in
  backup) backup "$@" ;;
  restore) restore "$@" ;;
  *) usage ;;
esac
