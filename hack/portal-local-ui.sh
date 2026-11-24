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

# Runs the portal of the Tilt environment with the generic UI (list, detail
# and create views) of a local portal-ui-lib checkout, to try a change of it
# in the browser.
#
#   hack/portal-local-ui.sh           build the checkout and roll it out
#   hack/portal-local-ui.sh --reset   back to the released portal image
#
# The generic UI is one file the portal serves,
# /assets/platform-mesh-portal-ui-wc.js. The script builds it in the checkout
# (npm run build), puts it into an image on top of the portal image the
# cluster runs, loads that image into the kind cluster and points the portal
# Deployment at it. The rest of the portal stays what was released, so a
# change outside the web components (the portal shell) is not picked up.
#
# Reload the portal without cache afterwards. Tilt owns the Deployment and
# puts the released image back when it deploys the portal again; run the
# script again then.
#
#   --ui-lib DIR   the portal-ui-lib checkout (default: ../portal-ui-lib,
#                  next to this repository)
#   --skip-build   use the bundle already built in the checkout
#   --reset        roll back to the released image and stop
#
# Environment:
#   KIND_CONTEXT        kube context of the kind cluster (default: kind-platform-mesh)
#   KIND_CLUSTER        name of the kind cluster (default: the context without "kind-")
#   PLATFORM_NAMESPACE  namespace of the installation (default: platform-mesh-system)
#   PORTAL_UI_NODE      Node.js the checkout is built with when the installed
#                       one is older, fetched with npx (default: 24.15.0)

set -euo pipefail

die() { echo "$@" >&2; exit 1; }

repo="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
context="${KIND_CONTEXT:-kind-platform-mesh}"
cluster="${KIND_CLUSTER:-${context#kind-}}"
namespace="${PLATFORM_NAMESPACE:-platform-mesh-system}"
node_version="${PORTAL_UI_NODE:-24.15.0}"

# Marks the images of this script; what precedes it is the released image.
suffix="-local-ui-"
bundle="platform-mesh-portal-ui-wc.js"
bundle_in_image="/app/frontend/dist/frontend/browser/assets/$bundle"

ui_lib="$repo/../portal-ui-lib"
build=true
reset=false
while [ $# -gt 0 ]; do
    case "$1" in
        --ui-lib) ui_lib="${2:?--ui-lib needs a directory}"; shift 2 ;;
        --skip-build) build=false; shift ;;
        --reset) reset=true; shift ;;
        -h | --help) awk 'NR >= 17 && !/^#/ { exit } NR >= 17 { sub(/^# ?/, ""); print }' "$0"; exit 0 ;;
        *) die "unknown argument $1" ;;
    esac
done

kc() { kubectl --context "$context" -n "$namespace" "$@"; }

current="$(kc get deployment portal -o jsonpath='{.spec.template.spec.containers[?(@.name=="portal")].image}')" \
    || die "no portal Deployment in $namespace of $context; is the Tilt environment up?"
[ -n "$current" ] || die "the portal Deployment has no container named portal"
released="${current%%"$suffix"*}"

roll_out() {
    kc set image deployment/portal "portal=$1"
    kc rollout status deployment/portal --timeout=3m
}

if $reset; then
    if [ "$current" = "$released" ]; then
        echo "the portal already runs the released image $released"
    else
        roll_out "$released"
    fi
    exit 0
fi

[ -f "$ui_lib/package.json" ] || die "no portal-ui-lib checkout at $ui_lib (--ui-lib DIR)"
ui_lib="$(cd "$ui_lib" && pwd)"

if $build; then
    echo "building $ui_lib"
    # The Angular CLI refuses a Node.js older than it was built for.
    have="$(node --version 2>/dev/null | sed 's/^v//' || true)"
    if [ -n "$have" ] && [ "$(printf '%s\n%s\n' "$node_version" "$have" | sort -V | head -n1)" = "$node_version" ]; then
        (cd "$ui_lib" && npm run build)
    else
        echo "Node.js ${have:-none} is older than $node_version, building with node@$node_version from npx"
        (cd "$ui_lib" && npx --yes -p "node@$node_version" -c 'npm run build')
    fi
fi

built="$ui_lib/dist-wc/assets/$bundle"
[ -f "$built" ] || die "no bundle at $built; build the checkout (without --skip-build)"

context_dir="$(mktemp -d)"
trap 'rm -rf "$context_dir"' EXIT
cp "$built" "$context_dir/$bundle"
cat > "$context_dir/Dockerfile" <<EOF
FROM $released
COPY --chown=1001:1001 $bundle $bundle_in_image
EOF

# A tag of its own per build, so that the Deployment rolls.
image="$released$suffix$(date +%Y%m%d%H%M%S)"
arch="$(kubectl --context "$context" get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')"

echo "building $image"
docker build --quiet --platform "linux/$arch" -t "$image" "$context_dir" > /dev/null
kind load docker-image "$image" --name "$cluster"
roll_out "$image"

cat <<EOF

The portal runs the generic UI of $ui_lib.
Reload it without cache. Back to the released image: $0 --reset
EOF
