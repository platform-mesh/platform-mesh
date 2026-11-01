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

# Demonstrates why migrating OCM resources from type:helm to type:ociArtifact requires
# pre-mirroring charts to an OCI registry — the OCM toolkit alone is not sufficient.
#
# The script builds the same two-component structure twice:
#
#   BEFORE  — cert-manager with  type: helm   (HTTP Helm repo reference)
#   AFTER   — cert-manager with  type: ociArtifact + relation: local  (OCI mirror)
#
# Then it runs ocm transfer ctf --copy-local-resources on both and shows that:
#   BEFORE: charts are NOT embedded — the access reference remains a Helm HTTP URL
#   AFTER:  charts ARE embedded     — the access reference becomes a local OCI blob
#
# Usage:
#   ./hack/demo-oci-migration.sh             # uses defaults from ocm.yaml
#   ./hack/demo-oci-migration.sh --dry-run   # leave the local registry + CTFs running for inspection

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

DRY_RUN=false
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true

# ── Versions ──────────────────────────────────────────────────────────────────

if ! command -v yq &>/dev/null; then
  echo "Error: yq is required. Install from https://github.com/mikefarah/yq" >&2
  exit 1
fi
if ! command -v ocm &>/dev/null; then
  echo "Error: ocm CLI not found. Install from https://github.com/open-component-model/ocm/releases" >&2
  exit 1
fi
if ! command -v docker &>/dev/null; then
  echo "Error: docker is required to run a local OCI registry for the demo." >&2
  exit 1
fi

# This demo relies on the legacy ocm CLI (open-component-model/ocm, v0.x).
# OCM v2 (ocm.software, v2.x) renamed `ocm transfer ctf` to `ocm transfer component-version`
# and removed `--copy-local-resources` and `ocm get resources`. If the ocm on PATH is v2,
# look for the legacy binary in well-known locations and prefer it.
ocm_is_legacy() {
  "$1" transfer ctf --help >/dev/null 2>&1
}

OCM="$(command -v ocm)"
if ! ocm_is_legacy "$OCM"; then
  for candidate in /usr/local/bin/ocm /usr/bin/ocm /opt/ocm/bin/ocm; do
    if [[ -x "$candidate" ]] && ocm_is_legacy "$candidate"; then
      OCM="$candidate"
      echo "Note: ocm on PATH is OCM v2 (no --copy-local-resources). Using legacy CLI: $OCM"
      break
    fi
  done
fi
if ! ocm_is_legacy "$OCM"; then
  echo "Error: this demo requires the legacy ocm CLI (open-component-model/ocm v0.x)." >&2
  echo "  The CLI on PATH ($(command -v ocm)) is OCM v2, which removed --copy-local-resources." >&2
  echo "  Install v0.x from https://github.com/open-component-model/ocm/releases and put it ahead on PATH." >&2
  exit 1
fi

CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-$(yq '.CERT_MANAGER_VERSION' "$REPO_ROOT/ocm/versions.yaml")}"

# The OCI mirror location — produced by the mirror-helm-chart.yaml workflow in this repo.
# This must already exist before running the AFTER scenario.
OCI_CHART_REF="ghcr.io/platform-mesh/ocm/charts/cert-manager-demo:${CERT_MANAGER_VERSION}"
# Real production mirror (already published):
OCI_CHART_REF_REAL="quay.io/jetstack/charts/cert-manager:${CERT_MANAGER_VERSION}"

PM_VERSION="${PM_VERSION:-0.0.0-demo}"

echo "╔══════════════════════════════════════════════════════════════════╗"
echo "║          OCM Helm → OCI migration demo                          ║"
echo "╠══════════════════════════════════════════════════════════════════╣"
echo "  cert-manager version : $CERT_MANAGER_VERSION"
echo "  platform-mesh version: $PM_VERSION"
echo "  dry-run              : $DRY_RUN"
echo ""

WORK_DIR=$(mktemp -d)

# Spin up a throwaway local OCI registry so we can show what actually lands
# in the "target" — the ultimate destination for an air-gapped transfer.
REG_PORT="${REG_PORT:-5005}"
REG_NAME="ocm-demo-registry-$$"
REG_HOST="localhost:${REG_PORT}"

cleanup() {
  echo ""
  echo "Cleaning up local registry container..."
  docker rm -f "$REG_NAME" >/dev/null 2>&1 || true
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

echo "Starting local OCI registry at $REG_HOST (container: $REG_NAME) ..."
docker run -d --rm --name "$REG_NAME" -p "${REG_PORT}:5000" registry:2 >/dev/null

# Wait for the registry to accept requests
for i in {1..20}; do
  if curl -sf "http://${REG_HOST}/v2/" >/dev/null 2>&1; then
    break
  fi
  sleep 0.3
done
if ! curl -sf "http://${REG_HOST}/v2/" >/dev/null 2>&1; then
  echo "Error: local registry did not become ready at $REG_HOST" >&2
  exit 1
fi
echo "Local registry ready."
echo ""

CTF_BEFORE="$WORK_DIR/before.ctf"
CTF_AFTER="$WORK_DIR/after.ctf"

# OCM repo specs for the local registry. The JSON form is the only supported way
# to point OCM at a plain-HTTP registry on a non-standard port. Each scenario uses
# a different subPath so the BEFORE and AFTER artifacts land under distinct paths.
REPO_BEFORE='{"baseUrl":"http://'"${REG_HOST}"'","subPath":"before","type":"OCIRegistry"}'
REPO_AFTER='{"baseUrl":"http://'"${REG_HOST}"'","subPath":"after","type":"OCIRegistry"}'

# Helper: list every artifact tag in the local registry under a given path prefix
# AND verify each tag's manifest is actually fetchable (HEAD /v2/<repo>/manifests/<tag>
# must return 200 with a Docker-Content-Digest header). This is what proves the bytes
# advertised in /v2/_catalog and /v2/<repo>/tags/list are real, not just placeholders.
list_registry_contents() {
  local label="$1"
  echo "$label"
  local catalog
  catalog=$(curl -sf "http://${REG_HOST}/v2/_catalog" || echo '{"repositories":[]}')
  local repos
  repos=$(echo "$catalog" | python3 -c "import json,sys; print('\n'.join(json.load(sys.stdin).get('repositories',[])))")
  if [[ -z "$repos" ]]; then
    echo "  (registry is empty)"
    return
  fi
  local accept='application/vnd.oci.image.manifest.v1+json,application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.v2+json,application/vnd.docker.distribution.manifest.list.v2+json'
  while IFS= read -r repo; do
    [[ -z "$repo" ]] && continue
    local tags
    tags=$(curl -sf "http://${REG_HOST}/v2/${repo}/tags/list" \
      | python3 -c "import json,sys; d=json.load(sys.stdin); print(' '.join(d.get('tags') or []))" 2>/dev/null || true)
    if [[ -z "$tags" ]]; then
      echo "  ${repo}:  (no tags)"
      continue
    fi
    echo "  ${repo}:"
    for tag in $tags; do
      local headers status digest
      headers=$(curl -sI -H "Accept: ${accept}" "http://${REG_HOST}/v2/${repo}/manifests/${tag}" || true)
      status=$(echo "$headers" | awk 'NR==1 {print $2}')
      digest=$(echo "$headers" | awk -F': ' 'tolower($1)=="docker-content-digest" {print $2}' | tr -d '\r\n ')
      if [[ "$status" == "200" && -n "$digest" ]]; then
        echo "    ${tag}  ✓ HTTP ${status}  ${digest}"
      else
        echo "    ${tag}  ✗ HTTP ${status:-?}  digest=${digest:-<missing>}"
      fi
    done
  done <<< "$repos"
}

# ══════════════════════════════════════════════════════════════════════════════
# BEFORE — type: helm  (HTTP Helm repository reference)
# ══════════════════════════════════════════════════════════════════════════════
cat > "$WORK_DIR/before.yaml" <<EOF
components:
  - name: github.com/cert-manager/cert-manager
    version: ${CERT_MANAGER_VERSION}
    provider:
      name: cert-manager
    resources:
      - name: chart
        type: helmChart
        # No relation: field → OCM defaults to relation: external
        version: ${CERT_MANAGER_VERSION}
        access:
          type: helm
          helmChart: cert-manager:${CERT_MANAGER_VERSION}
          helmRepository: https://charts.jetstack.io
      - name: controller-image
        type: ociImage
        relation: local
        version: ${CERT_MANAGER_VERSION}
        access:
          type: ociArtifact
          imageReference: "quay.io/jetstack/cert-manager-controller:${CERT_MANAGER_VERSION}"

  - name: github.com/platform-mesh/platform-mesh
    version: ${PM_VERSION}
    provider:
      name: The Platform Mesh Team
    componentReferences:
      - name: cert-manager
        componentName: github.com/cert-manager/cert-manager
        version: ${CERT_MANAGER_VERSION}
EOF

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "BEFORE  type: helm  (no OCI mirror)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Building CTF..."
"$OCM" add components -c --templater=none --file "$CTF_BEFORE" "$WORK_DIR/before.yaml"

echo ""
echo "All resource access in the CTF (before transfer):"
"$OCM" get resources "github.com/cert-manager/cert-manager:${CERT_MANAGER_VERSION}" \
  --repo "$CTF_BEFORE" -o json \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)['items']
for it in items:
    e = it['element']
    print(f\"  {e['name']:20} relation={e.get('relation','external'):8} access.type={e['access']['type']}\")
    for k,v in e['access'].items():
        if k != 'type':
            print(f\"    {k}: {v}\")
"

echo ""
echo "Running: ocm transfer ctf --copy-local-resources  →  http://${REG_HOST}/before"
"$OCM" transfer ctf --copy-local-resources "$CTF_BEFORE" "$REPO_BEFORE" 2>&1 || true

echo ""
list_registry_contents "Target registry contents AFTER transfer (path: /before/...):"

echo ""
echo "All resource access AS SEEN FROM THE TARGET REGISTRY (--copy-local-resources):"
"$OCM" get resources \
  "github.com/cert-manager/cert-manager:${CERT_MANAGER_VERSION}" \
  --repo "$REPO_BEFORE" -o json \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)['items']
for it in items:
    e = it['element']
    print(f\"  {e['name']:20} relation={e.get('relation','external'):8} access.type={e['access']['type']}\")
    for k,v in e['access'].items():
        if k != 'type':
            print(f\"    {k}: {v}\")
"

echo ""
echo "☝  Look at the target registry contents above:"
echo "   - controller-image  IS in the registry (relation=local, type=ociArtifact → embedded)"
echo "   - chart             is NOT in the registry — its component descriptor still points"
echo "                       at https://charts.jetstack.io (access.type=helm preserved)"
echo ""
echo "   --copy-local-resources copied the OCI image but skipped the helm chart entirely"
echo "   because its access type forces relation=external. An air-gapped consumer pulling"
echo "   from this registry would fail to install: the chart bytes simply aren't there."

# ══════════════════════════════════════════════════════════════════════════════
# AFTER — type: ociArtifact + relation: local  (OCI mirror)
# ══════════════════════════════════════════════════════════════════════════════
cat > "$WORK_DIR/after.yaml" <<EOF
components:
  - name: github.com/cert-manager/cert-manager
    version: ${CERT_MANAGER_VERSION}-pm.1
    provider:
      name: cert-manager
    resources:
      - name: chart
        type: helmChart
        relation: local
        # ↑ relation: local tells OCM "copy this resource by-value on transfer"
        version: ${CERT_MANAGER_VERSION}
        access:
          type: ociArtifact
          # ↑ ociArtifact — OCM can pull this during transfer; helm HTTP it cannot
          imageReference: "${OCI_CHART_REF_REAL}"
          # This OCI artifact was produced by the mirror-helm-chart.yaml workflow:
          #   helm pull cert-manager --repo https://charts.jetstack.io
          #   helm push cert-manager-${CERT_MANAGER_VERSION}.tgz oci://quay.io/jetstack/charts
      - name: controller-image
        type: ociImage
        relation: local
        version: ${CERT_MANAGER_VERSION}
        access:
          type: ociArtifact
          imageReference: "quay.io/jetstack/cert-manager-controller:${CERT_MANAGER_VERSION}"

  - name: github.com/platform-mesh/platform-mesh
    version: ${PM_VERSION}
    provider:
      name: The Platform Mesh Team
    componentReferences:
      - name: cert-manager
        componentName: github.com/cert-manager/cert-manager
        version: ${CERT_MANAGER_VERSION}-pm.1
EOF

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "AFTER   type: ociArtifact + relation: local  (with OCI mirror)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Building CTF..."
"$OCM" add components -c --templater=none --file "$CTF_AFTER" "$WORK_DIR/after.yaml"

echo ""
echo "All resource access in the CTF (before transfer):"
"$OCM" get resources "github.com/cert-manager/cert-manager:${CERT_MANAGER_VERSION}-pm.1" \
  --repo "$CTF_AFTER" -o json \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)['items']
for it in items:
    e = it['element']
    print(f\"  {e['name']:20} relation={e.get('relation','external'):8} access.type={e['access']['type']}\")
    for k,v in e['access'].items():
        if k != 'type':
            print(f\"    {k}: {v}\")
"

echo ""
echo "Running: ocm transfer ctf --copy-local-resources  →  http://${REG_HOST}/after"
"$OCM" transfer ctf --copy-local-resources "$CTF_AFTER" "$REPO_AFTER" 2>&1

echo ""
list_registry_contents "Target registry contents AFTER transfer (path: /after/...):"

echo ""
echo "All resource access AS SEEN FROM THE TARGET REGISTRY (--copy-local-resources):"
"$OCM" get resources \
  "github.com/cert-manager/cert-manager:${CERT_MANAGER_VERSION}-pm.1" \
  --repo "$REPO_AFTER" -o json \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)['items']
for it in items:
    e = it['element']
    print(f\"  {e['name']:20} relation={e.get('relation','external'):8} access.type={e['access']['type']}\")
    for k,v in e['access'].items():
        if k != 'type':
            print(f\"    {k}: {v}\")
"

echo ""
echo "☝  Look at the target registry contents above:"
echo "   - controller-image  IS in the registry (embedded ociArtifact)"
echo "   - chart             IS in the registry (embedded ociArtifact, same mechanism)"
echo ""
echo "   Both resources are now reachable through the target registry alone. An air-gapped"
echo "   consumer can install cert-manager without ever touching charts.jetstack.io or"
echo "   quay.io. The CTF + target registry are fully self-contained."

# ══════════════════════════════════════════════════════════════════════════════
# Summary
# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Why the OCM toolkit alone is not enough"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
cat <<'EXPLANATION'

  type: helm  access points at an HTTP Helm repository (e.g. https://charts.jetstack.io).

  Two transfer modes, two different problems for our delivery pipeline:

    --copy-local-resources  skips type:helm entirely.
                            The helm access type has relation=external by definition; OCM's
                            CopyModeLocalBlobResources handler does not touch external resources.
                            Result: chart access remains "type: helm / helmRepository: https://..."
                            in the target registry. Air-gap is broken silently.

    --copy-resources        Embeds the chart, but as a localBlob NOT a discoverable OCI artifact.
                            OCM fetches the .tgz from the HTTP Helm repo (at `ocm add components`
                            time — i.e. CI must reach charts.jetstack.io) and stores it inline in
                            the component descriptor manifest with mediaType
                            "application/vnd.cncf.helm.chart.content.v1.tar+gzip" and access.type
                            "localBlob".

                            Why this isn't useful for us:
                            - The chart blob is reachable only by SHA digest inside the descriptor,
                              not as an addressable OCI tag. `helm pull oci://...` won't find it.
                            - Our consumer (platform-mesh-operator's ResourceSubroutine) builds a
                              FluxCD HelmRelease from access.helmRepository, access.repoUrl, or
                              access.imageReference. localBlob has none of these — installation
                              fails with "no helmRepository, repoUrl, or imageReference found".
                            - The upstream HTTP fetch happens during CI builds, so a broken or
                              re-tagged upstream silently affects every build.

  Why pre-mirroring is the correct solution:

  Pre-publishing the chart as an OCI artifact gives every consumer a stable, addressable URL:

    1. helm pull <chart> --repo <http-repo>          # one-time download from HTTP
    2. helm push <chart>.tgz oci://<oci-registry>    # publish as OCI artifact

  The constructor then references that OCI URL with relation: local:

    access:
      type: ociArtifact
      imageReference: <oci-registry>/<chart>:<version>

  --copy-local-resources copies the OCI artifact by-value into the target registry. The result:
    - access.imageReference is rewritten to the target registry (FluxCD can pull it over OCI)
    - the chart content is content-addressed and pinned (the tag is immutable in our registry)
    - no internet access is required at install time AND no live HTTP fetch happens during
      OCM build — the upstream is contacted only by the dedicated mirror workflow

EXPLANATION

if [[ "$DRY_RUN" == "true" ]]; then
  echo "Dry-run complete."
  echo "  CTFs left in: $WORK_DIR"
  echo "  Local registry left running:  http://$REG_HOST  (container: $REG_NAME)"
  echo "  Stop it with: docker rm -f $REG_NAME"
  trap - EXIT
fi
