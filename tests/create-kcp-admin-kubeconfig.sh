#!/bin/bash

set -e

# KUBECONFIG must point to the cluster running Platform Mesh (the operator
# and platform-mesh-system namespace).  Default falls back to the current
# context so local kind setups still work without extra configuration.
KUBECONFIG="${KUBECONFIG:-}"

mkdir -p "$PWD/.secret/kcp"

KUBECTL_ARGS=()
if [[ -n "$KUBECONFIG" ]]; then
  KUBECTL_ARGS=(--kubeconfig "$KUBECONFIG")
fi

kubectl "${KUBECTL_ARGS[@]}" get secret kubeconfig-kcp-admin \
  -n platform-mesh-system \
  -o=jsonpath='{.data.kubeconfig}' \
  | base64 -d > "$PWD/.secret/kcp/admin.kubeconfig"

# TODO: remove once the root CA rotation issue is resolved in the installation.
# root-server-ca was issued by a previous root-ca that is no longer in the cluster,
# so TLS verification fails for clients without the original root-ca in their trust store.
yq -i "del(.clusters[].cluster.\"certificate-authority-data\")" "$PWD/.secret/kcp/admin.kubeconfig"
yq -i "(.clusters[].cluster.\"insecure-skip-tls-verify\") = true" "$PWD/.secret/kcp/admin.kubeconfig"
