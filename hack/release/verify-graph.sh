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

# Checks a published Platform Mesh component graph the way a consumer sees it:
#
#   1. the aggregate resolves recursively (every referenced component version
#      exists in the repository),
#   2. every component version built from this checkout at --version (the
#      aggregate, chart and service components, the image components) verifies
#      against --signature,
#   3. every chart in the graph can be pulled, and every chart built from this
#      repository (version == --version) that is not a library chart templates
#      with its default values. Third-party charts are only pulled: some need
#      values to template, and their authors test them.
#
# job-publish.yml runs it against ghcr.io after a release or prerelease,
# hack/e2e/verify.sh against the local registry of a pull request build; both
# see the same checks.
#
# Usage:
#   verify-graph.sh --version V --ocm-repo REPO [options]
#
# Options:
#   --version V         the Platform Mesh version to check (required)
#   --image-version V   version of the image components (default: --version)
#   --ocm-repo REPO     OCM repository the graph was published to (required);
#                       http://... for a plain-HTTP registry
#   --chart-repo REPO   where the charts are (default <ocm-repo>/helm-charts,
#                       like build-aggregate.sh)
#   --component NAME    aggregate component name (default github.com/platform-mesh/platform-mesh)
#   --signature NAME    verify signatures with this name (public key from the
#                       ocm config); without it signatures are not checked
#   --skip-charts       do not pull and template the charts
#
# Needs the ocm config (OCM_CONFIG or ~/.ocmconfig) with the registry
# credentials and, for --signature, the public certificate (ocm-config.sh).

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

VERSION=""; IMAGE_VERSION=""; OCM_REPO=""; CHART_REPO=""
COMPONENT="github.com/platform-mesh/platform-mesh"; SIGNATURE=""; SKIP_CHARTS=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --image-version) IMAGE_VERSION="$2"; shift 2 ;;
    --ocm-repo) OCM_REPO="$2"; shift 2 ;;
    --chart-repo) CHART_REPO="$2"; shift 2 ;;
    --component) COMPONENT="$2"; shift 2 ;;
    --signature) SIGNATURE="$2"; shift 2 ;;
    --skip-charts) SKIP_CHARTS=true; shift ;;
    -h|--help) sed -n '16,/^$/p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done
[[ -n "$VERSION" ]] || { echo "--version is required" >&2; exit 1; }
[[ -n "$OCM_REPO" ]] || { echo "--ocm-repo is required" >&2; exit 1; }
IMAGE_VERSION="${IMAGE_VERSION:-$VERSION}"
CHART_REPO="${CHART_REPO:-$OCM_REPO/helm-charts}"

log() { echo "[$(date '+%H:%M:%S')] $*" >&2; }
die() { echo "error: $*" >&2; exit 1; }

command -v jq >/dev/null || die "jq is required"
source hack/release/tools.sh
require_tools ocm helm yq

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

# ---------------------------------------------------------------------------
# What should be there: the constructor this checkout generates for the version
# ---------------------------------------------------------------------------
hack/release/build-aggregate.sh --version "$VERSION" --image-version "$IMAGE_VERSION" \
  --ocm-repo "$OCM_REPO" --chart-repo "$CHART_REPO" --component "$COMPONENT" \
  --generate-only --output "$tmp/constructor.yaml" >/dev/null
constructor="$("$YQ" -o=json '.' "$tmp/constructor.yaml")"

# ---------------------------------------------------------------------------
# 1. The graph resolves
# ---------------------------------------------------------------------------
log "resolving $COMPONENT:$VERSION in $OCM_REPO"
if ! "$OCM" get component-version "$OCM_REPO//$COMPONENT:$VERSION" --recursive; then
  fail "$COMPONENT:$VERSION does not resolve recursively"
fi

# ---------------------------------------------------------------------------
# 2. Signatures of everything built from this checkout at this version
# ---------------------------------------------------------------------------
if [[ -n "$SIGNATURE" ]]; then
  {
    jq -r --arg v "$VERSION" '.components[] | select(.version == $v) | "\(.name):\(.version)"' <<<"$constructor"
    "$YQ" -r '.components | to_entries[] | select(.value.image) | "github.com/platform-mesh/images/" + .key' ocm/components.yaml \
      | sed "s/\$/:$IMAGE_VERSION/"
  } | sort -u | while read -r cv; do
    [[ -n "$cv" ]] || continue
    if "$OCM" verify component-version --signature "$SIGNATURE" "$OCM_REPO//$cv" >/dev/null 2>"$tmp/verify.err"; then
      echo "verified $cv"
    else
      echo "FAIL: signature $SIGNATURE of $cv: $(tail -1 "$tmp/verify.err")" >&2
      echo "$cv" >> "$tmp/unverified"
    fi
  done
  if [[ -s "$tmp/unverified" ]]; then
    failures=$((failures + $(wc -l < "$tmp/unverified")))
  fi
else
  log "no --signature given; skipping signature verification"
fi

# ---------------------------------------------------------------------------
# 3. Charts are installable artifacts
# ---------------------------------------------------------------------------
if [[ "$SKIP_CHARTS" != "true" ]]; then
  plain_http=()
  while IFS= read -r ref; do
    [[ -n "$ref" ]] || continue
    # http://host/path/name:version -> oci://host/path/name, version
    bare="${ref#http://}"; bare="${bare#https://}"; bare="${bare#oci://}"
    [[ "$ref" == http://* ]] && plain_http=(--plain-http)
    chart_ref="${bare%:*}"; chart_version="${bare##*:}"; name="${chart_ref##*/}"
    dest="$tmp/charts/$name"; mkdir -p "$dest"
    if ! "$HELM" pull "oci://$chart_ref" --version "$chart_version" --destination "$dest" --untar ${plain_http[@]+"${plain_http[@]}"} >/dev/null 2>"$tmp/pull.err"; then
      fail "pull $chart_ref:$chart_version: $(tail -1 "$tmp/pull.err")"
      continue
    fi
    if [[ "$chart_version" != "$VERSION" ]]; then
      echo "ok: $name $chart_version (third-party chart, pulled)"
      continue
    fi
    if [[ "$("$YQ" -r '.type // "application"' "$dest/$name/Chart.yaml")" == "library" ]]; then
      echo "ok: $name $chart_version (library chart, not templated)"
      continue
    fi
    if "$HELM" template verify "$dest/$name" >/dev/null 2>"$tmp/template.err"; then
      echo "ok: $name $chart_version"
    else
      fail "template $name:$chart_version: $(grep -m1 -i error "$tmp/template.err" || tail -1 "$tmp/template.err")"
    fi
  done < <(jq -r '.components[].resources[]? | select(.type == "helmChart") | .access.imageReference' <<<"$constructor" | sort -u)
fi

if [[ $failures -gt 0 ]]; then
  die "$failures check(s) failed for $COMPONENT:$VERSION"
fi
log "ok: $COMPONENT:$VERSION in $OCM_REPO"
