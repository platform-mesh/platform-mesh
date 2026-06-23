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

set -euo pipefail

set -a # automatically export all variables from .env
source .env.searchindex-test
set +a

usage() {
  cat <<'EOF'
Usage: manual-test-searchindex.sh [options]

Manual SearchIndex test for local kcp Platform Mesh setup.
Default target workspace is root:orgs.

Options:
  --run-operator      Start search-operator locally for this test
  --cleanup           Delete the SearchIndex at the end (also verifies index deletion if enabled)
  --no-verify-os      Skip OpenSearch verification via curl
  -h, --help          Show this help

Environment overrides:
  KCP_KUBECONFIG      kcp kubeconfig path
  KCP_SERVER          kcp workspace server URL (default: https://localhost:8443/clusters/root:orgs)
  SEARCHINDEX_NAME    Name of SearchIndex resource. In root:orgs this should be the org workspace name.
  INDEX_PREFIX        spec.indexPrefix value
  TIMEOUT_SECONDS     Wait timeout for status.indexName
  OPENSEARCH_URL      OpenSearch base URL, e.g. https://localhost:9200
  OPENSEARCH_USERNAME OpenSearch username
  OPENSEARCH_PASSWORD OpenSearch password
  OPENSEARCH_INSECURE true to use curl -k
EOF
}

while (($# > 0)); do
  case "$1" in
    --run-operator)
      RUN_OPERATOR=true
      shift
      ;;
    --cleanup)
      CLEANUP=true
      shift
      ;;
    --no-verify-os)
      VERIFY_OPENSEARCH=false
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 2
      ;;
  esac
done

require_bin() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required binary: $1" >&2
    exit 1
  fi
}

require_bin kubectl

if [[ "${VERIFY_OPENSEARCH}" == "true" ]]; then
  require_bin curl
fi

if [[ ! -f "${KCP_KUBECONFIG}" ]]; then
  echo "Kubeconfig not found: ${KCP_KUBECONFIG}" >&2
  exit 1
fi

export KUBECONFIG="${KCP_KUBECONFIG}"

operator_pid=""

cleanup_background() {
  if [[ -n "${operator_pid}" ]]; then
    kill "${operator_pid}" >/dev/null 2>&1 || true
    wait "${operator_pid}" >/dev/null 2>&1 || true
  fi
}

trap cleanup_background EXIT

if [[ "${RUN_OPERATOR}" == "true" ]]; then
  echo "Starting search-operator locally..."
  (
    cd "${REPO_ROOT}"
    KCP_KUBECONFIG="${KCP_KUBECONFIG}" go run ./cmd/main.go --kcp-kubeconfig="${KCP_KUBECONFIG}"
  ) &
  operator_pid="$!"
  sleep 5
fi

echo "Testing SearchIndex in workspace server: ${KCP_SERVER}"
echo "Searchindex name: ${SEARCHINDEX_NAME}"
echo "Index prefix: ${INDEX_PREFIX}"

echo $KUBECONFIG
# Updates the kcp path in the local kubeconfig

echo "Checking SearchIndex API availability..."
if ! kubectl api-resources --server="${KCP_SERVER}" | grep "searchindices"; then
  echo "SearchIndex API is not available on ${KCP_SERVER}" >&2
  exit 1
fi

echo "Applying SearchIndex resource..."
cat <<EOF | kubectl apply --server="${KCP_SERVER}" -f -
apiVersion: core.platform-mesh.io/v1alpha1
kind: SearchIndex
metadata:
  name: ${SEARCHINDEX_NAME}
spec:
  indexPrefix: ${INDEX_PREFIX}
EOF

echo "Waiting for reconciliation (status.indexName)..."
index_name=""
deadline=$((SECONDS + TIMEOUT_SECONDS))
while ((SECONDS < deadline)); do
  index_name="$(kubectl get searchindices.core.platform-mesh.io "${SEARCHINDEX_NAME}" --server="${KCP_SERVER}" -o jsonpath='{.status.indexName}' 2>/dev/null || true)"
  if [[ -n "${index_name}" ]]; then
    break
  fi
  sleep 2
done

if [[ -z "${index_name}" ]]; then
  echo "Timed out waiting for status.indexName after ${TIMEOUT_SECONDS}s" >&2
  kubectl get searchindices.core.platform-mesh.io "${SEARCHINDEX_NAME}" --server="${KCP_SERVER}" -o yaml || true
  exit 1
fi

echo "Reconciled index name: ${index_name}"
echo "Current SearchIndex resource:"
kubectl get searchindices.core.platform-mesh.io "${SEARCHINDEX_NAME}" --server="${KCP_SERVER}" -o yaml

if [[ "${VERIFY_OPENSEARCH}" == "true" ]]; then
  if [[ -z "${OPENSEARCH_URL:-}" ]]; then
    echo "Skipping OpenSearch verification because OPENSEARCH_URL is not set."
  else
    curl_args=()
    if [[ "${OPENSEARCH_INSECURE:-false}" == "true" ]]; then
      curl_args+=("-k")
    fi
    if [[ -n "${OPENSEARCH_USERNAME:-}" ]]; then
      curl_args+=("-u" "${OPENSEARCH_USERNAME}:${OPENSEARCH_PASSWORD:-}")
    fi

    echo "Verifying index exists in OpenSearch..."
    curl -fsS "${curl_args[@]}" "${OPENSEARCH_URL%/}/${index_name}" >/dev/null
    echo "OpenSearch index exists: ${index_name}"
  fi
fi

if [[ "${CLEANUP}" == "true" ]]; then
  echo "Deleting SearchIndex resource..."
  kubectl delete searchindices.core.platform-mesh.io "${SEARCHINDEX_NAME}" --server="${KCP_SERVER}" --wait=true

  if [[ "${VERIFY_OPENSEARCH}" == "true" && -n "${OPENSEARCH_URL:-}" ]]; then
    curl_args=()
    if [[ "${OPENSEARCH_INSECURE:-false}" == "true" ]]; then
      curl_args+=("-k")
    fi
    if [[ -n "${OPENSEARCH_USERNAME:-}" ]]; then
      curl_args+=("-u" "${OPENSEARCH_USERNAME}:${OPENSEARCH_PASSWORD:-}")
    fi

    echo "Verifying index deletion in OpenSearch..."
    if curl -fsS "${curl_args[@]}" "${OPENSEARCH_URL%/}/${index_name}" >/dev/null 2>&1; then
      echo "Index still exists after SearchIndex deletion: ${index_name}" >&2
      exit 1
    fi
    echo "OpenSearch index deletion confirmed: ${index_name}"
  fi
fi

echo "Manual SearchIndex test completed successfully."
