#!/bin/bash

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

# Copies the CRDs that `task generate` writes into each component's config/
# directory into the *-crds charts that ship them, applying the chart-specific
# templating on the way. The component's generated CRD is the single source of
# truth; never hand-edit the chart copy.
#
# Runs as the last step of `task generate` and standalone as
# `task charts:sync-crds`. CI runs it and fails when the result differs from
# what is committed (verify.yml, job verify).
#
# Usage: sync-crds.sh            (from the repository root)
#
# Only CRDs installed on the management cluster by a chart are listed here.
# APIExports/APIResourceSchemas (config/resources, manifests/kcp) are applied
# by the operators themselves and are not chart content.

set -euo pipefail

cd "$(dirname "$0")/../.."

# sync <source CRD> <chart file> [transform]
#
# transform:
#   (none)      verbatim copy
#   scope       spec.scope becomes {{ .Values.crds.scope }}
#   conversion  an optional spec.conversion.strategy block driven by
#               .Values.conversion.strategy is inserted under spec:
sync() {
  local src="$1" dest="$2" transform="${3:-}"

  if [ ! -f "$src" ]; then
    echo "sync-crds: missing $src (run the component's 'task generate' first)" >&2
    exit 1
  fi
  mkdir -p "$(dirname "$dest")"

  case "$transform" in
    "")
      cp "$src" "$dest"
      ;;
    scope)
      if ! grep -qE '^  scope: (Cluster|Namespaced)$' "$src"; then
        echo "sync-crds: no top-level 'scope:' line in $src to template" >&2
        exit 1
      fi
      sed -E 's/^  scope: (Cluster|Namespaced)$/  scope: {{ .Values.crds.scope }}/' "$src" > "$dest"
      ;;
    conversion)
      if ! grep -qx 'spec:' "$src"; then
        echo "sync-crds: no top-level 'spec:' line in $src to template" >&2
        exit 1
      fi
      awk '
        { print }
        !done && $0 == "spec:" {
          print "  {{- if .Values.conversion.strategy }}"
          print "  conversion:"
          print "    strategy: {{ .Values.conversion.strategy }}"
          print "  {{- end }}"
          done = 1
        }
      ' "$src" > "$dest"
      ;;
    *)
      echo "sync-crds: unknown transform '$transform'" >&2
      exit 1
      ;;
  esac
  echo "   $src -> $dest"
}

echo ">> syncing generated CRDs into charts"

# platform-mesh-operator: the CRDs the operator chart installs via crds/.
sync operators/platform-mesh-operator/config/crd/core.platform-mesh.io_platformmeshes.yaml \
     charts/platform-mesh-operator-crds/crds/core.platform-mesh.io_platformmeshes.yaml
sync operators/platform-mesh-operator/config/crd/providers.platform-mesh.io_managedproviders.yaml \
     charts/platform-mesh-operator-crds/crds/providers.platform-mesh.io_managedproviders.yaml

# kubernetes-graphql-gateway: ClusterAccess, with the values-driven conversion
# strategy override (see charts/kubernetes-graphql-gateway-crds/values.yaml).
sync services/kubernetes-graphql-gateway/config/crd/gateway.platform-mesh.io_clusteraccesses.yaml \
     charts/kubernetes-graphql-gateway-crds/templates/cluster-access.yaml \
     conversion

# extension-manager-operator: ContentConfiguration, scope chosen by values.
sync operators/extension-manager-operator/config/crd/bases/ui.platform-mesh.io_contentconfigurations.yaml \
     charts/extension-manager-operator-crds/templates/ui.platform-mesh.io_contentconfigurations.yaml \
     scope

# A changed chart CRD changes the helm-unittest snapshots of that chart AND of
# every chart that depends on it (e.g. kubernetes-graphql-gateway bundles
# kubernetes-graphql-gateway-crds). Point at the follow-up instead of letting
# chart-unittest in CI be the first to notice.
if git rev-parse --is-inside-work-tree >/dev/null 2>&1 && ! git diff --quiet -- charts/; then
  echo ">> chart CRDs changed; refresh the unittest snapshots with: task charts:update-changed"
fi
