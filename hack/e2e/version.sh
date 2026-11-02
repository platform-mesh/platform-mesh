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

# The version of the snapshot built from this checkout by hack/e2e/:
#
#   images     <next>-snapshot.<build>.g<sha>          e.g. 26.11.0-snapshot.1730000000.g1a2b3c4
#   aggregate  <next>-snapshot.<build>.<seq>.g<sha>    e.g. 26.11.0-snapshot.1730000000.0.g1a2b3c4
#
# <next> is the next release (hack/release/next-version.sh). "snapshot" sorts
# above "rc" and "dev", so a snapshot can be installed over the latest release
# candidate or prerelease as an upgrade (the ocm-k8s-toolkit denies
# downgrades), and below the final release. <build> is the GitHub Actions run
# number, or the time `images.sh build` ran; <sha> the commit the images were
# built from, recorded at the same time (so a commit made while the flow runs
# does not change the version under it); <seq> counts aggregate builds on
# the same images, so that `task e2e:ocm BUMP=true` after a chart change
# publishes a new, higher version without rebuilding the images.
#
# Usage: version.sh [--image] [--new-build] [--bump]
#   --image      print the image version
#   --new-build  start a new build number (images.sh build does this)
#   --bump       next aggregate version for the same images

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

mode=aggregate
while [[ $# -gt 0 ]]; do
  case "$1" in
    --image) mode=image; shift ;;
    --new-build)
      save_state build "${GITHUB_RUN_NUMBER:-$(date +%s)}"
      save_state sha "$(git -C "$E2E_ROOT" rev-parse --short=7 HEAD)"
      save_state seq 0
      shift ;;
    --bump) save_state seq "$(( $(cat "$E2E_STATE/seq" 2>/dev/null || echo 0) + 1 ))"; shift ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done

if [[ -f "$E2E_STATE/build" ]]; then
  build="$(cat "$E2E_STATE/build")"
elif [[ -n "${GITHUB_RUN_NUMBER:-}" ]]; then
  build="$GITHUB_RUN_NUMBER"   # CI jobs share the run number without sharing state
else
  die "no build number yet: run images.sh build (task e2e:images) first"
fi
seq="$(cat "$E2E_STATE/seq" 2>/dev/null || echo 0)"
next="$("$E2E_ROOT/hack/release/next-version.sh")"
sha="$(cat "$E2E_STATE/sha" 2>/dev/null || git -C "$E2E_ROOT" rev-parse --short=7 HEAD)"

case "$mode" in
  image) echo "${next}-snapshot.${build}.g${sha}" ;;
  aggregate) echo "${next}-snapshot.${build}.${seq}.g${sha}" ;;
esac
