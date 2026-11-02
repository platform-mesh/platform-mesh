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

# Builds and publishes the Platform Mesh OCM component graph for the current
# checkout: the Helm charts, their OCM components, the third-party wrapper
# components and the top-level aggregate. The release and prerelease GitHub
# workflows call this file with different parameters.
#
# Platform Mesh has one version (RFC 015): every chart, every chart component
# and every image component built from this repository carries --version. The
# charts in git say 0.0.0; hack/release/stage-charts.sh writes the real
# version into a copy under ocm/.generated/charts/ before packaging.
#
# For publishing, `helm registry login` and an ocm config with registry
# credentials must be set up.
#
# Inputs (all taken from the checkout):
#   charts/*/Chart.yaml             the charts; appVersion of charts wrapping
#                                   components from other repositories
#   ocm/components.yaml             which charts belong to components built here
#   ocm/charts/<chart>.yaml         per-chart OCM settings (see ocm/charts/README.md)
#   ocm/component-constructor.yaml  template: aggregate + third-party wrappers
#   ocm/versions.yaml               third-party version pins
#
# Usage:
#   build-aggregate.sh --version <YY.MM.PP> [options]
#
# Options:
#   --version V         the Platform Mesh version (required)
#   --image-version V   version of the images built from this repository
#                       (default: --version; their image tag is v<V>). Lets a
#                       working-tree build reuse the images of a release.
#   --component NAME    aggregate component name (default github.com/platform-mesh/platform-mesh)
#   --ocm-repo REPO     OCM repository to publish to and resolve references from
#                       (default ghcr.io/platform-mesh)
#   --chart-repo REPO   OCI repository charts are pushed to, without oci://
#                       (default <ocm-repo>/helm-charts, the namespace charts
#                       have always been published to; images live under
#                       <ocm-repo>/platform-mesh/, so the two never share a
#                       repository)
#   --repo-url URL      git repository URL recorded as source (default from origin)
#   --commit SHA        git commit recorded as source (default HEAD)
#   --signature NAME    sign every newly published component version with this
#                       signature (key from the ocm config, see ocm-config.sh)
#   --mirror-from REPO  copy referenced image components that are missing in
#                       --ocm-repo from this OCM repository (for test setups)
#   --skip-charts       do not package/push charts (assume they are published)
#   --ctf PATH          build into a CTF archive instead of --ocm-repo; nothing
#                       is pushed. Existence checks still run against --ocm-repo.
#   --generate-only     only write the constructor and exit
#   --output FILE       where to write the generated constructor
#                       (default ocm/.generated/constructor.yaml)

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

VERSION=""
IMAGE_VERSION=""
COMPONENT="github.com/platform-mesh/platform-mesh"
OCM_REPO="ghcr.io/platform-mesh"
CHART_REPO=""
REPO_URL=""
COMMIT=""
SIGNATURE=""
MIRROR_FROM=""
SKIP_CHARTS=false
CTF=""
GENERATE_ONLY=false
OUTPUT="ocm/.generated/constructor.yaml"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --image-version) IMAGE_VERSION="$2"; shift 2 ;;
    --component) COMPONENT="$2"; shift 2 ;;
    --ocm-repo) OCM_REPO="$2"; shift 2 ;;
    --chart-repo) CHART_REPO="$2"; shift 2 ;;
    --repo-url) REPO_URL="$2"; shift 2 ;;
    --commit) COMMIT="$2"; shift 2 ;;
    --signature) SIGNATURE="$2"; shift 2 ;;
    --mirror-from) MIRROR_FROM="$2"; shift 2 ;;
    --skip-charts) SKIP_CHARTS=true; shift ;;
    --ctf) CTF="$2"; shift 2 ;;
    --generate-only) GENERATE_ONLY=true; shift ;;
    --output) OUTPUT="$2"; shift 2 ;;
    -h|--help) sed -n '16,/^$/p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done

[[ -n "$VERSION" ]] || { echo "--version is required" >&2; exit 1; }
IMAGE_VERSION="${IMAGE_VERSION:-$VERSION}"
IMAGE_VERSION="${IMAGE_VERSION#v}"
CHART_REPO="${CHART_REPO:-$OCM_REPO/helm-charts}"
CHART_REPO="${CHART_REPO#oci://}"
COMMIT="${COMMIT:-$(git rev-parse HEAD)}"
if [[ -z "$REPO_URL" ]]; then
  REPO_URL="$(git remote get-url origin 2>/dev/null | sed -E 's#^git@github.com:#https://github.com/#; s#\.git$##')"
fi
TARGET="${CTF:+ctf::$CTF}"
TARGET="${TARGET:-$OCM_REPO}"

log() { echo "[$(date '+%H:%M:%S')] $*" >&2; }
die() { echo "error: $*" >&2; exit 1; }

command -v jq >/dev/null || die "jq is required"
# Pinned ocm/helm/yq (tools/Taskfile.yaml), installed on demand into bin/.
source hack/release/tools.sh
require_tools ocm helm yq

# ---------------------------------------------------------------------------
# Versions
# ---------------------------------------------------------------------------
# Third-party pins and the aggregate version/name become environment variables
# for the constructor template and for ${...} in ocm/charts/*.yaml.
# shellcheck disable=SC1090
source <(hack/release/export-versions.sh --export)
export VERSION IMAGE_VERSION COMPONENT_NAME="$COMPONENT" REPO_URL COMMIT

# ---------------------------------------------------------------------------
# Stage the charts with the real version and collect their metadata
# ---------------------------------------------------------------------------
mkdir -p "$(dirname "$OUTPUT")"
STAGED="$(dirname "$OUTPUT")/charts"
hack/release/stage-charts.sh "$STAGED" "$VERSION" "$IMAGE_VERSION" >/dev/null
inhouse_charts="$("$YQ" '.components[] | select(has("chart")) | .chart' ocm/components.yaml | sed 's#^charts/##')"

# charts_json: [{name, version, publish, flat, hasImages, hasRefs, imageComponent, imageVersion, settings}]
charts_json="[]"
for chart_yaml in "$STAGED"/*/Chart.yaml; do
  name="$(basename "$(dirname "$chart_yaml")")"
  settings="ocm/charts/$name.yaml"
  [[ -f "$settings" ]] || settings=""

  app_version="$("$YQ" -r '.appVersion // ""' "$chart_yaml")"
  publish="$( [[ -n "$settings" ]] && "$YQ" -r '.publish != false' "$settings" || echo true)"
  flat="$( [[ -n "$settings" ]] && "$YQ" -r '.flat // false' "$settings" || echo false)"
  has_images="$( [[ -n "$settings" ]] && "$YQ" -r 'has("images")' "$settings" || echo false)"
  has_refs="$( [[ -n "$settings" ]] && "$YQ" -r 'has("extraReferences")' "$settings" || echo false)"
  wants_image="$( [[ -n "$settings" ]] && "$YQ" -r '.image != false' "$settings" || echo true)"

  # Which image component the service component references, if any:
  #   - charts of components built here: images/<chart> at the image version
  #   - wrapper charts with an `images:` inventory: images/<chart> at the chart version
  #   - charts of components from other repositories: images/<chart> at their appVersion
  image_component=""; image_version=""
  if [[ "$wants_image" == "true" ]]; then
    if grep -qx "$name" <<<"$inhouse_charts"; then
      image_component="github.com/platform-mesh/images/$name"; image_version="$IMAGE_VERSION"
    elif [[ "$has_images" == "true" ]]; then
      image_component="github.com/platform-mesh/images/$name"; image_version="$VERSION"
    elif [[ -n "$app_version" && "$app_version" != "0.0.0" ]]; then
      image_component="github.com/platform-mesh/images/$name"; image_version="$app_version"
    fi
  fi

  charts_json="$(jq -c \
    --arg name "$name" --arg version "$VERSION" \
    --argjson publish "$publish" --argjson flat "$flat" --argjson hasImages "$has_images" --argjson hasRefs "$has_refs" \
    --arg imageComponent "$image_component" --arg imageVersion "$image_version" --arg settings "$settings" \
    '. + [{name:$name, version:$version, publish:$publish, flat:$flat, hasImages:$hasImages, hasRefs:$hasRefs, imageComponent:$imageComponent, imageVersion:$imageVersion, settings:$settings}]' <<<"$charts_json")"
done
log "staged $(jq length <<<"$charts_json") charts at version $VERSION (images: $IMAGE_VERSION) in $STAGED"

# ---------------------------------------------------------------------------
# Generate the constructor
# ---------------------------------------------------------------------------
# Start from the template (aggregate + third-party wrappers), substituting the
# variables ourselves so the output is a plain, inspectable document.
template_json="$("$YQ" -o=json '.' ocm/component-constructor.yaml | envsubst)"
unresolved="$(grep -oE '\$\{[A-Z0-9_]+\}' <<<"$template_json" | sort -u || true)"
[[ -z "$unresolved" ]] || die "unresolved variables in ocm/component-constructor.yaml: $(tr '\n' ' ' <<<"$unresolved")"

generated="$template_json"
while IFS= read -r chart; do
  name="$(jq -r .name <<<"$chart")"
  version="$(jq -r .version <<<"$chart")"
  [[ "$(jq -r .publish <<<"$chart")" == "true" ]] || continue
  settings="$(jq -r .settings <<<"$chart")"
  image_component="$(jq -r .imageComponent <<<"$chart")"
  image_version="$(jq -r .imageVersion <<<"$chart")"

  chart_resource="$(jq -n --arg ref "$CHART_REPO/$name:$version" --arg v "$version" \
    '{name:"chart", type:"helmChart", relation:"external", version:$v, access:{type:"ociArtifact", imageReference:$ref}}')"
  chart_source="$(jq -n --arg v "$version" --arg commit "$COMMIT" --arg url "$REPO_URL" \
    '{name:"chart", type:"git", version:$v, access:{type:"gitHub", repoUrl:$url, commit:$commit}}')"
  provider='{name:"The Platform Mesh Team"}'

  service="$(jq -n --arg name "github.com/platform-mesh/$name" --arg v "$version" --argjson src "$chart_source" \
    "{name:\$name, version:\$v, provider:$provider, sources:[\$src]}")"

  if [[ "$(jq -r .flat <<<"$chart")" == "true" ]]; then
    # Chart resource directly in the service component, no helm-charts/ layer, no source.
    service="$(jq --argjson r "$chart_resource" '.resources = [$r] | del(.sources)' <<<"$service")"
  else
    chart_component="$(jq -n --arg name "github.com/platform-mesh/helm-charts/$name" --arg v "$version" \
      --argjson r "$chart_resource" --argjson src "$chart_source" \
      "{name:\$name, version:\$v, provider:$provider, resources:[\$r], sources:[\$src]}")"
    generated="$(jq --argjson c "$chart_component" '.components += [$c]' <<<"$generated")"
    service="$(jq --arg name "github.com/platform-mesh/helm-charts/$name" --arg v "$version" \
      '.componentReferences = [{name:"chart", componentName:$name, version:$v}]' <<<"$service")"
  fi

  if [[ -n "$image_component" ]]; then
    service="$(jq --arg name "$image_component" --arg v "$image_version" \
      '.componentReferences += [{name:"image", componentName:$name, version:$v}]' <<<"$service")"
  fi

  if [[ "$(jq -r .hasImages <<<"$chart")" == "true" ]]; then
    # Third-party image inventory of a wrapper chart, versioned like the chart.
    images="$("$YQ" -o=json '.images | map(.relation = "external")' "$settings" | envsubst)"
    image_comp="$(jq -n --arg name "$image_component" --arg v "$version" --argjson res "$images" \
      "{name:\$name, version:\$v, provider:$provider, resources:\$res}")"
    generated="$(jq --argjson c "$image_comp" '.components += [$c]' <<<"$generated")"
  fi

  if [[ "$(jq -r .hasRefs <<<"$chart")" == "true" ]]; then
    refs="$("$YQ" -o=json '.extraReferences' "$settings" | envsubst)"
    service="$(jq --argjson refs "$refs" '.componentReferences += $refs' <<<"$service")"
  fi

  generated="$(jq --argjson c "$service" '.components += [$c]' <<<"$generated")"
done < <(jq -c '.[]' <<<"$charts_json")

{
  echo "# GENERATED by hack/release/build-aggregate.sh for $COMPONENT:$VERSION at $COMMIT. Do not edit."
  "$YQ" -P '.' <<<"$generated"
} > "$OUTPUT"
log "constructor with $(jq '.components | length' <<<"$generated") components written to $OUTPUT"

if [[ "$GENERATE_ONLY" == "true" ]]; then
  exit 0
fi

# ---------------------------------------------------------------------------
# Helpers talking to the registry
# ---------------------------------------------------------------------------
cv_exists() { # <repo> <component> <version>
  "$OCM" get component-version "$1//$2:$3" -o json >/dev/null 2>&1
}

chart_exists() { # <name> <version>
  "$HELM" show chart "oci://$CHART_REPO/$1" --version "$2" >/dev/null 2>&1
}

# ---------------------------------------------------------------------------
# Charts: package and push whatever is not published yet
# ---------------------------------------------------------------------------
if [[ "$SKIP_CHARTS" != "true" ]]; then
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  while IFS= read -r chart; do
    name="$(jq -r .name <<<"$chart")"; version="$(jq -r .version <<<"$chart")"
    if chart_exists "$name" "$version"; then
      log "chart $name:$version already published"
      continue
    fi
    log "packaging and pushing chart $name:$version"
    "$HELM" package "$STAGED/$name" --destination "$tmp" >/dev/null
    retry 3 "$HELM" push "$tmp/$name-$version.tgz" "oci://$CHART_REPO" >/dev/null
  done < <(jq -c '.[]' <<<"$charts_json")
fi

# ---------------------------------------------------------------------------
# Referenced components that are not built here must already exist
# ---------------------------------------------------------------------------
missing=()
while IFS=$'\t' read -r cname cver; do
  # Components defined in this constructor are fine.
  if jq -e --arg n "$cname" --arg v "$cver" '.components[] | select(.name == $n and .version == $v)' <<<"$generated" >/dev/null; then
    continue
  fi
  if cv_exists "$OCM_REPO" "$cname" "$cver"; then
    continue
  fi
  if [[ -n "$MIRROR_FROM" ]] && cv_exists "$MIRROR_FROM" "$cname" "$cver"; then
    log "mirroring $cname:$cver from $MIRROR_FROM"
    retry 3 "$OCM" transfer component-version "$MIRROR_FROM//$cname:$cver" "$OCM_REPO" >/dev/null
    continue
  fi
  missing+=("$cname:$cver")
done < <(jq -r '.components[].componentReferences[]? | select(.componentName != "github.com/gardener/etcd-druid") | [.componentName, .version] | @tsv' <<<"$generated" | sort -u)

if [[ ${#missing[@]} -gt 0 ]]; then
  echo "error: the following referenced component versions do not exist in $OCM_REPO:" >&2
  for m in "${missing[@]}"; do
    echo "  - $m" >&2
    case "$m" in
      github.com/platform-mesh/images/*)
        c="${m#github.com/platform-mesh/images/}"; c="${c%%:*}"; v="${m##*:}"
        if grep -qx "$c" <<<"$inhouse_charts"; then
          echo "      -> built from this repository; the release workflow publishes it before the aggregate (--image-version $IMAGE_VERSION)" >&2
        else
          echo "      -> built outside this repository; check that version $v was released and update charts/$c/Chart.yaml (appVersion)" >&2
        fi ;;
    esac
  done
  exit 1
fi

# etcd-druid is published as an OCM component by Gardener and copied into our
# repository with all referenced resources.
etcd_druid_ref="github.com/gardener/etcd-druid"
if jq -e --arg n "$etcd_druid_ref" '.components[].componentReferences[]? | select(.componentName == $n)' <<<"$generated" >/dev/null; then
  if cv_exists "$OCM_REPO" "$etcd_druid_ref" "$GARDENER_ETCD_DRUID_VERSION"; then
    log "etcd-druid $GARDENER_ETCD_DRUID_VERSION already present"
  else
    log "copying etcd-druid $GARDENER_ETCD_DRUID_VERSION from Gardener's registry"
    "$OCM" transfer component-version --copy-resources --recursive \
      "europe-docker.pkg.dev/gardener-project/releases//$etcd_druid_ref:$GARDENER_ETCD_DRUID_VERSION" "$OCM_REPO"
  fi
fi

# ---------------------------------------------------------------------------
# Immutability: existing component versions must match what we would build
# ---------------------------------------------------------------------------
# Published component versions are never replaced. If one of ours already
# exists, its resources and references must be identical to the constructor's,
# otherwise someone changed content without bumping a version.
new_components=()
conflicts=0
while IFS=$'\t' read -r cname cver; do
  if ! cv_exists "$OCM_REPO" "$cname" "$cver"; then
    new_components+=("$cname:$cver")
    continue
  fi
  # Resources and references identify a component version. The source commit
  # only matters for the aggregate (re-tagging a released version from another
  # commit); sub-components keep the commit they were first published from.
  shape='{res: ([.resources[]? | {name, version}] | sort_by(.name)),
          refs: ([.componentReferences[]? | {name, componentName, version}] | sort_by(.name))}'
  if [[ "$cname" == "$COMPONENT" ]]; then
    shape='{res: ([.resources[]? | {name, version}] | sort_by(.name)),
            refs: ([.componentReferences[]? | {name, componentName, version}] | sort_by(.name)),
            src: ([.sources[]? | {name, commit: .access.commit}] | sort_by(.name))}'
  fi
  want="$(jq -c --arg n "$cname" --arg v "$cver" ".components[] | select(.name == \$n and .version == \$v) | $shape" <<<"$generated")"
  have="$("$OCM" get component-version "$OCM_REPO//$cname:$cver" -o json | jq -c "(if type == \"array\" then .[0] else . end) | .component | $shape")"
  if [[ "$want" != "$have" ]]; then
    echo "error: $cname:$cver already exists in $OCM_REPO with different content:" >&2
    diff <(jq . <<<"$have") <(jq . <<<"$want") | sed 's/^/    /' >&2 || true
    if [[ "$cver" == "$VERSION" ]]; then
      echo "    -> $VERSION was already published from a different commit; versions are immutable, use a new one" >&2
    else
      echo "    -> bump the PM_* version of this wrapper in ocm/versions.yaml" >&2
    fi
    conflicts=$((conflicts + 1))
  fi
done < <(jq -r '.components[] | [.name, .version] | @tsv' <<<"$generated")
[[ $conflicts -eq 0 ]] || exit 1

if [[ ${#new_components[@]} -eq 0 ]]; then
  log "$COMPONENT:$VERSION and everything it references are already published; nothing to do"
  : > "$(dirname "$OUTPUT")/published.txt"
  exit 0
fi

# ---------------------------------------------------------------------------
# Build, sign, done
# ---------------------------------------------------------------------------
log "building ${#new_components[@]} new component version(s) into $TARGET"
"$OCM" add component-versions \
  --repository "$TARGET" \
  --constructor "$OUTPUT" \
  --component-version-conflict-policy skip \
  --skip-reference-digest-processing=false

if [[ -n "$SIGNATURE" ]]; then
  for cv in "${new_components[@]}"; do
    log "signing $cv"
    "$OCM" sign component-version --signature "$SIGNATURE" "$TARGET//$cv" >/dev/null
  done
fi

log "done: $COMPONENT:$VERSION published to $TARGET"
printf '%s\n' "${new_components[@]}" > "$(dirname "$OUTPUT")/published.txt"
