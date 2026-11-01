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

# Publishes the OCM component github.com/platform-mesh/images/<component> for
# an image built from this repository: the image (by digest), its SBOMs and
# the git source. The aggregate built by build-aggregate.sh references these
# components from the charts' service components.
#
# Usage:
#   build-image-component.sh --component NAME --version V --image-ref REF \
#       --sbom-dir DIR --ocm-repo REPO [--signature NAME] [--repo-url URL] [--commit SHA]
#
#   --image-ref   image reference including digest (ghcr.io/.../name@sha256:...)
#   --sbom-dir    directory containing sbom-cyclonedx.json and sbom-spdx.json
#   --signature   sign the component with this signature (key from the ocm
#                 config, see ocm-config.sh)
#
# The component is versioned V; the image resource inside it is versioned vV,
# i.e. exactly the image tag: consumers that cannot use OCM localization (the
# platform-mesh-operator's own bootstrap) build the image reference from the
# resource version.
#
# Published versions are immutable: if the component version already exists in
# --ocm-repo, the script fails. Needs an ocm config with registry credentials.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

COMPONENT=""; VERSION=""; IMAGE_REF=""; SBOM_DIR=""; OCM_REPO=""; SIGNATURE=""; REPO_URL=""; COMMIT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --component) COMPONENT="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --image-ref) IMAGE_REF="$2"; shift 2 ;;
    --sbom-dir) SBOM_DIR="$2"; shift 2 ;;
    --ocm-repo) OCM_REPO="$2"; shift 2 ;;
    --signature) SIGNATURE="$2"; shift 2 ;;
    --repo-url) REPO_URL="$2"; shift 2 ;;
    --commit) COMMIT="$2"; shift 2 ;;
    -h|--help) sed -n '16,/^$/p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done
for v in COMPONENT VERSION IMAGE_REF SBOM_DIR OCM_REPO; do
  [[ -n "${!v}" ]] || { echo "--$(tr 'A-Z_' 'a-z-' <<<"$v") is required" >&2; exit 1; }
done
COMMIT="${COMMIT:-$(git rev-parse HEAD)}"
if [[ -z "$REPO_URL" ]]; then
  REPO_URL="$(git remote get-url origin 2>/dev/null | sed -E 's#^git@github.com:#https://github.com/#; s#\.git$##')"
fi
[[ "$IMAGE_REF" == *@sha256:* ]] || { echo "--image-ref must include the digest" >&2; exit 1; }
[[ -f "$SBOM_DIR/sbom-cyclonedx.json" && -f "$SBOM_DIR/sbom-spdx.json" ]] || { echo "SBOMs missing in $SBOM_DIR" >&2; exit 1; }

source hack/release/tools.sh
require_tools ocm

log() { echo "[$(date '+%H:%M:%S')] $*" >&2; }

name="github.com/platform-mesh/images/$COMPONENT"
if "$OCM" get component-version "$OCM_REPO//$name:$VERSION" -o json >/dev/null 2>&1; then
  echo "error: $name:$VERSION already exists in $OCM_REPO; published versions are immutable" >&2
  exit 1
fi

# OCM only reads file inputs from below the constructor's directory.
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
cp "$SBOM_DIR/sbom-cyclonedx.json" "$SBOM_DIR/sbom-spdx.json" "$tmp/"
cat > "$tmp/constructor.yaml" <<EOF
components:
  - name: "$name"
    version: "$VERSION"
    provider:
      name: The Platform Mesh Team
    sources:
      - name: source
        type: git
        version: "$VERSION"
        access:
          type: gitHub
          repoUrl: "$REPO_URL"
          commit: "$COMMIT"
    resources:
      - name: image
        type: ociImage
        relation: external
        version: "v$VERSION"
        access:
          type: ociArtifact
          imageReference: "$IMAGE_REF"
      - name: sbom-cyclonedx
        type: sbom
        input:
          type: file
          path: sbom-cyclonedx.json
          mediaType: application/vnd.cyclonedx+json
      - name: sbom-spdx
        type: sbom
        input:
          type: file
          path: sbom-spdx.json
          mediaType: application/spdx+json
EOF

# Build into a CTF first so the signature covers the local blobs, then transfer.
log "building $name:$VERSION"
"$OCM" add component-versions --repository "ctf::$tmp/transport.ctf" --constructor "$tmp/constructor.yaml" >/dev/null
if [[ -n "$SIGNATURE" ]]; then
  log "signing $name:$VERSION"
  "$OCM" sign component-version --signature "$SIGNATURE" "ctf::$tmp/transport.ctf//$name:$VERSION" >/dev/null
fi
log "publishing to $OCM_REPO"
retry 3 "$OCM" transfer component-version "ctf::$tmp/transport.ctf//$name:$VERSION" "$OCM_REPO" >/dev/null
log "done: $name:$VERSION published to $OCM_REPO"
