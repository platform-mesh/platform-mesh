#!/usr/bin/env bash
set -euo pipefail

# Migrate OpenFGA tuples from a backup directory by replacing old KCP cluster IDs
# with current ones from the live KCP state.
#
# The script discovers all workspaces, extracts LogicalCluster IDs, then builds a
# mapping from old IDs (found in the backup tuples) to new IDs (from the current KCP).
# It identifies workspaces by matching workspace names referenced in tuples to the
# current workspace tree.
#
# Usage:
#   docs/migration-0.3/migrate-openfga-tuples.sh <backup-dir> [output-dir]
#
# Environment:
#   KUBECONFIG_KCP  - path to KCP admin kubeconfig (default: .secret/kcp/admin.kubeconfig)
#   KCP_SERVER      - KCP API server URL (default: https://localhost:8443)
#   DRY_RUN         - set to "true" to only print the mapping without writing files

usage() {
  cat <<'EOF'
Migrate OpenFGA tuples from a backup by replacing old KCP cluster IDs with
current ones from the live KCP state.

USAGE:
  migrate-openfga-tuples.sh <backup-dir> [output-dir]

ARGUMENTS:
  backup-dir    Directory containing *-tuples.json files exported from OpenFGA.
  output-dir    Where to write migrated files (default: <backup-dir>/migrated).

ENVIRONMENT VARIABLES:
  KUBECONFIG_KCP  Path to KCP admin kubeconfig (default: .secret/kcp/admin.kubeconfig)
  KCP_SERVER      KCP API server URL (default: https://localhost:8443)
  FGA_SERVER_URL  OpenFGA API URL (default: http://localhost:8080)
  DRY_RUN         Set to "true" to print the ID mapping without writing or applying.

HOW IT WORKS:
  1. Discovers all KCP workspaces recursively from root.
  2. Reads the LogicalCluster ID (kcp.io/cluster annotation) for each workspace.
  3. Extracts old 16-char cluster IDs from *-tuples.json files in backup-dir.
  4. Matches each old ID to a workspace by checking which workspace's children
     correspond to the workspace names that follow the ID in tuples
     (pattern: CLUSTER_ID/WORKSPACE_NAME).
  5. Falls back to APIExport matching for IDs referencing
     apis_kcp_io_apiexport:CLUSTER_ID/export-name.
  6. Replaces old IDs with new IDs in all tuple files and writes to output-dir.
  7. Writes the migrated tuples to OpenFGA via the fga CLI, matching store names
     from filenames (e.g. default-tuples.json → store "default").

EXAMPLES:
  # Standard migration
  docs/migration-0.3/migrate-openfga-tuples.sh local-setup/backup/openfga

  # Custom output directory
  docs/migration-0.3/migrate-openfga-tuples.sh local-setup/backup/openfga /tmp/migrated-tuples

  # Dry run — inspect the mapping without writing anything
  DRY_RUN=true docs/migration-0.3/migrate-openfga-tuples.sh local-setup/backup/openfga

  # Use a remote KCP server
  KCP_SERVER=https://kcp.example.com:8443 \
    docs/migration-0.3/migrate-openfga-tuples.sh local-setup/backup/openfga

PREREQUISITES:
  - kubectl with access to the KCP API server
  - KCP admin kubeconfig (generate with local-setup/scripts/createKcpAdminKubeconfig.sh)
  - fga CLI (OpenFGA CLI) with access to the OpenFGA server
  - jq for JSON transformation
  - grep with PCRE support (-P flag)
  - python3 (for JSON pretty-printing in diff output)
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

BACKUP_DIR="${1:?Usage: $0 <backup-dir> [output-dir]. Run with --help for details.}"
OUTPUT_DIR="${2:-${BACKUP_DIR}/migrated}"
KUBECONFIG_KCP="${KUBECONFIG_KCP:-.secret/kcp/admin.kubeconfig}"
KCP_SERVER="${KCP_SERVER:-https://localhost:8443}"
DRY_RUN="${DRY_RUN:-false}"

if [[ ! -d "$BACKUP_DIR" ]]; then
  echo "Error: Backup directory not found: $BACKUP_DIR" >&2
  exit 1
fi

if [[ ! -f "$KUBECONFIG_KCP" ]]; then
  echo "Error: KCP admin kubeconfig not found at $KUBECONFIG_KCP" >&2
  echo "Run 'local-setup/scripts/createKcpAdminKubeconfig.sh' first." >&2
  exit 1
fi

KC="kubectl --kubeconfig=$KUBECONFIG_KCP"

get_logical_cluster_id() {
  local ws="$1"
  $KC get logicalclusters --server "${KCP_SERVER}/clusters/${ws}" \
    -o jsonpath='{.items[0].metadata.annotations.kcp\.io/cluster}' 2>/dev/null || true
}

discover_workspaces() {
  local parent="$1"
  echo "$parent"
  local children
  children=$($KC get workspace --server "${KCP_SERVER}/clusters/${parent}" \
    -o jsonpath='{.items[*].metadata.name}' 2>/dev/null) || true
  for child in $children; do
    discover_workspaces "${parent}:${child}"
  done
}

echo "=== Discovering KCP workspaces ==="
WORKSPACES=()
while IFS= read -r ws; do
  WORKSPACES+=("$ws")
done < <(discover_workspaces "root")

echo "Found ${#WORKSPACES[@]} workspaces:"
printf "  %s\n" "${WORKSPACES[@]}"

echo ""
echo "=== Building workspace → cluster ID mapping ==="

declare -A WS_TO_CLUSTER_ID
for ws in "${WORKSPACES[@]}"; do
  cluster_id=$(get_logical_cluster_id "$ws")
  if [[ -n "$cluster_id" ]]; then
    WS_TO_CLUSTER_ID["$ws"]="$cluster_id"
    echo "  $ws → $cluster_id"
  fi
done

echo ""
echo "=== Extracting old cluster IDs from backup tuples ==="

# Cluster IDs in tuples follow these patterns:
#   core_platform-mesh_io_account:CLUSTER_ID/WORKSPACE_NAME
#   role:core_platform-mesh_io_account/CLUSTER_ID/WORKSPACE_NAME/ROLE
#   apis_kcp_io_apiexport:CLUSTER_ID/EXPORT_NAME
OLD_CLUSTER_IDS=()
for f in "$BACKUP_DIR"/*-tuples.json; do
  [[ -f "$f" ]] || continue
  # Extract IDs: alphanumeric strings of 16 chars that appear after / or : in tuple values
  ids=$(grep -oP '(?<=[:/])[a-z0-9]{16}(?=/)' "$f" | sort -u || true)
  for id in $ids; do
    if [[ ! " ${OLD_CLUSTER_IDS[*]:-} " =~ " $id " ]]; then
      OLD_CLUSTER_IDS+=("$id")
    fi
  done
done

echo "Found old cluster IDs:"
printf "  %s\n" "${OLD_CLUSTER_IDS[@]}"

echo ""
echo "=== Matching old IDs to workspaces ==="

# For each old cluster ID, find which workspace name follows it in the tuples.
# Pattern: CLUSTER_ID/WORKSPACE_NAME — the workspace name tells us which parent
# workspace the ID belongs to.
declare -A OLD_TO_NEW_MAPPING

for old_id in "${OLD_CLUSTER_IDS[@]}"; do
  # Find workspace names that follow this cluster ID in tuples
  ws_names=()
  for f in "$BACKUP_DIR"/*-tuples.json; do
    [[ -f "$f" ]] || continue
    # Extract names after CLUSTER_ID/ (the workspace name part)
    names=$(grep -oP "(?<=${old_id}/)[a-zA-Z0-9_.-]+" "$f" | sort -u || true)
    for name in $names; do
      if [[ ! " ${ws_names[*]:-} " =~ " $name " ]]; then
        ws_names+=("$name")
      fi
    done
  done

  if [[ ${#ws_names[@]} -eq 0 ]]; then
    echo "  WARNING: No workspace names found after $old_id — skipping" >&2
    continue
  fi

  echo "  Old ID $old_id has child workspaces: ${ws_names[*]}"

  # Find which current workspace path contains these child workspaces.
  # The old_id is the cluster ID of the parent workspace.
  matched=false
  for ws in "${WORKSPACES[@]}"; do
    # Check if this workspace has the expected children
    children=$($KC get workspace --server "${KCP_SERVER}/clusters/${ws}" \
      -o jsonpath='{.items[*].metadata.name}' 2>/dev/null) || true

    match_count=0
    for name in "${ws_names[@]}"; do
      if [[ " $children " =~ " $name " ]]; then
        ((match_count++)) || true
      fi
    done

    if [[ $match_count -eq ${#ws_names[@]} && $match_count -gt 0 ]]; then
      new_id="${WS_TO_CLUSTER_ID[$ws]:-}"
      if [[ -n "$new_id" ]]; then
        OLD_TO_NEW_MAPPING["$old_id"]="$new_id"
        echo "    → Matched to workspace $ws (new ID: $new_id)"
        matched=true
        break
      fi
    fi
  done

  if [[ "$matched" == "false" ]]; then
    # Fallback: check if the ID is an APIExport provider workspace
    # (these appear as apis_kcp_io_apiexport:CLUSTER_ID/export-name)
    for ws in "${WORKSPACES[@]}"; do
      ws_cluster_id="${WS_TO_CLUSTER_ID[$ws]:-}"
      if [[ -z "$ws_cluster_id" ]]; then continue; fi

      # Check if this workspace has APIExports matching the names
      for name in "${ws_names[@]}"; do
        has_export=$($KC get apiexport "$name" \
          --server "${KCP_SERVER}/clusters/${ws}" \
          -o name 2>/dev/null) || true
        if [[ -n "$has_export" ]]; then
          OLD_TO_NEW_MAPPING["$old_id"]="$ws_cluster_id"
          echo "    → Matched to APIExport workspace $ws (new ID: $ws_cluster_id)"
          matched=true
          break 2
        fi
      done
    done
  fi

  if [[ "$matched" == "false" ]]; then
    echo "    → WARNING: Could not match old ID $old_id to any workspace" >&2
  fi
done

echo ""
echo "=== Cluster ID migration mapping ==="
for old_id in "${!OLD_TO_NEW_MAPPING[@]}"; do
  echo "  $old_id → ${OLD_TO_NEW_MAPPING[$old_id]}"
done

if [[ ${#OLD_TO_NEW_MAPPING[@]} -eq 0 ]]; then
  echo "No IDs to migrate. Either the IDs already match or no mapping could be established."
  exit 0
fi

if [[ "$DRY_RUN" == "true" ]]; then
  echo ""
  echo "DRY_RUN=true — not writing output files."
  exit 0
fi

echo ""
echo "=== Applying migrations ==="

mkdir -p "$OUTPUT_DIR"

for f in "$BACKUP_DIR"/*-tuples.json; do
  [[ -f "$f" ]] || continue
  filename=$(basename "$f")
  output_file="$OUTPUT_DIR/$filename"

  content=$(cat "$f")
  for old_id in "${!OLD_TO_NEW_MAPPING[@]}"; do
    new_id="${OLD_TO_NEW_MAPPING[$old_id]}"
    if [[ "$old_id" != "$new_id" ]]; then
      content="${content//$old_id/$new_id}"
    fi
  done

  echo "$content" > "$output_file"
  echo "  Written: $output_file"

  # Show what changed
  diff_output=$(diff <(python3 -m json.tool "$f") <(python3 -m json.tool "$output_file") || true)
  if [[ -n "$diff_output" ]]; then
    echo "$diff_output" | head -20
  else
    echo "    (no changes)"
  fi
done

echo ""
echo "=== Writing migrated tuples to OpenFGA ==="

FGA_SERVER_URL="${FGA_SERVER_URL:-http://localhost:8080}"

store_list=$(fga store list --server-url "$FGA_SERVER_URL" 2>/dev/null) || true
if [[ -z "$store_list" ]]; then
  echo "ERROR: Could not list OpenFGA stores at $FGA_SERVER_URL" >&2
  echo "Migrated files are in $OUTPUT_DIR — apply manually with:"
  echo "  fga tuple write --store-id=<ID> --file <file> --max-tuples-per-write 10"
  exit 1
fi

for f in "$OUTPUT_DIR"/*-tuples.json; do
  [[ -f "$f" ]] || continue
  filename=$(basename "$f")
  store_name="${filename%-tuples.json}"

  store_id=$(echo "$store_list" | jq -r ".stores[] | select(.name==\"${store_name}\") | .id")
  if [[ -z "$store_id" ]]; then
    echo "  WARNING: No store named '$store_name' found — skipping $filename" >&2
    continue
  fi

  # Delete existing tuples
  existing=$(fga tuple read --store-id="$store_id" --server-url "$FGA_SERVER_URL" \
    --max-pages 0 --output-format json 2>/dev/null) || true
  existing_count=$(echo "$existing" | jq '.tuples | length' 2>/dev/null || echo "0")
  if [[ "$existing_count" -gt 0 ]]; then
    echo "  Store '$store_name' ($store_id): deleting $existing_count existing tuples..."
    tmp_delete=$(mktemp --suffix=.json)
    echo "$existing" | jq '[.tuples[].key | {user, relation, object}]' > "$tmp_delete"
    fga tuple delete --store-id="$store_id" --server-url "$FGA_SERVER_URL" \
        --file "$tmp_delete" --max-tuples-per-write 1 || true
    rm -f "$tmp_delete"
    echo "    deleted."
  fi

  # Write migrated tuples one at a time to avoid "duplicate tuple in write" rejections
  # when multiple tuples share the same (object, relation) within a batch.
  tuple_count=$(jq '.tuples | length' "$f")
  echo "  Store '$store_name' ($store_id): writing $tuple_count tuples..."

  tmp_file=$(mktemp --suffix=.json)
  jq '[.tuples[].key | {user, relation, object}] | unique' "$f" > "$tmp_file"
  fga tuple write --store-id="$store_id" --server-url "$FGA_SERVER_URL" \
      --file "$tmp_file" --max-tuples-per-write 1 || true
  rm -f "$tmp_file"

  echo "    done."
done

echo ""
echo "=== Migration complete ==="
echo "Output directory: $OUTPUT_DIR"
