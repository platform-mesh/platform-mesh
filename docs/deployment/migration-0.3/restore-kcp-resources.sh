#!/usr/bin/env bash
set -euo pipefail

# Selectively restore KCP user data from a YAML export into a fresh 0.3 cluster.
# Skips operator-managed resources (APIExports, APIResourceSchemas, APIBindings, WorkspaceTypes).
#
# Usage:
#   docs/migration-0.3/restore-kcp-resources.sh <export-dir>
#
# Example:
#   docs/migration-0.3/restore-kcp-resources.sh docs/migration-0.3/kcp-exports/pre-migration

EXPORT_DIR="${1:?Usage: $0 <export-dir>}"
KUBECONFIG_KCP="${KUBECONFIG_KCP:-.secret/kcp/admin.kubeconfig}"
KCP_SERVER="${KCP_SERVER:-https://localhost:8443}"

if [[ ! -f "$KUBECONFIG_KCP" ]]; then
  echo "Error: KCP admin kubeconfig not found at $KUBECONFIG_KCP" >&2
  exit 1
fi

if [[ ! -d "$EXPORT_DIR" ]]; then
  echo "Error: Export directory not found: $EXPORT_DIR" >&2
  exit 1
fi

KC="kubectl --kubeconfig=$KUBECONFIG_KCP"

kcp_apply() {
  local ws="$1" file="$2"
  sed -e '/^\s*resourceVersion:/d' \
      -e '/^\s*uid:/d' \
      -e '/^\s*creationTimestamp:/d' \
      -e '/^\s*generation:/d' \
    < "$file" | \
  $KC apply --server-side --force-conflicts --server "${KCP_SERVER}/clusters/${ws}" -f - 2>&1
}

has_items() {
  local file="$1"
  [[ -f "$file" ]] && ! grep -q '^items: \[\]$' "$file" && grep -q '  name:' "$file"
}

USER_DATA_RESOURCES=(
  httpbins
  # stores
  # identityproviderconfigurations
  # accounts
  # accountinfos
  # authorizationmodels
  # invites
)

echo "=== Restoring KCP user data from $EXPORT_DIR ==="

for ws_dir in "$EXPORT_DIR"/root*; do
  [[ -d "$ws_dir" ]] || continue
  ws_name=$(basename "$ws_dir")
  ws="${ws_name//_/:}"

  [[ "$ws_name" == "k8s" ]] && continue

  echo ""
  echo "--- Workspace: $ws ---"

  for resource in "${USER_DATA_RESOURCES[@]}"; do
    file="$ws_dir/${resource}.yaml"
    if has_items "$file"; then
      echo "  Restoring $resource..."
      kcp_apply "$ws" "$file" || echo "  WARNING: failed to restore $resource in $ws"
    fi
  done
done

echo ""
echo "=== Restore complete ==="
