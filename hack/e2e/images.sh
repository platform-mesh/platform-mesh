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

# The component images of the snapshot, built exactly like the release builds
# them (`task images-<component>`, ocm/components.yaml), for the architecture
# of the Docker host unless E2E_PLATFORM says otherwise.
#
# Usage:
#   images.sh build [--keep-version] [component...]
#                                    build into the local Docker daemon; starts
#                                    a new build number (version.sh) unless
#                                    --keep-version (resume an interrupted build)
#   images.sh push  [component...]   push to the registry (registry.sh) and
#                                    record each image's digest for ocm.sh
#   images.sh save <dir> [component...]   `docker save` each image to <dir>/<component>.tar
#   images.sh load <dir>                  `docker load` every tar in <dir>
#
# Without components, all image components of ocm/components.yaml. save/load
# carry the images between CI jobs: the matrix job builds and saves, the
# install job loads and pushes.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cmd="${1:-}"; shift || true
dir=""; keep_version=false
case "$cmd" in save|load) dir="${1:?usage: $0 $cmd <dir> [component...]}"; shift ;; esac
if [[ "${1:-}" == "--keep-version" ]]; then keep_version=true; shift; fi
components=("$@")
if [[ ${#components[@]} -eq 0 ]]; then
  while IFS= read -r c; do components+=("$c"); done < <(image_components)
fi

platform="${E2E_PLATFORM:-linux/$(docker version -f '{{.Server.Arch}}')}"
image_ref() { echo "$(push_registry)/$IMAGE_REPO/$1:v$(image_version)"; }

case "$cmd" in
  build)
    [[ "$keep_version" == true ]] || "$E2E_ROOT/hack/e2e/version.sh" --new-build >/dev/null
    version="$(image_version)"
    log "building ${#components[@]} image(s) at v$version for $platform"
    for c in "${components[@]}"; do
      task -d "$E2E_ROOT" "images-$c" REGISTRY="$(push_registry)" REPO="$IMAGE_REPO" VERSION="v$version" PUSH=false PLATFORMS="$platform"
    done
    ;;
  push)
    mkdir -p "$E2E_STATE/images"
    for c in "${components[@]}"; do
      ref="$(image_ref "$c")"
      docker image inspect "$ref" >/dev/null 2>&1 || die "$ref not found: run images.sh build (task e2e:images) first"
      log "pushing $ref"
      docker push -q "$ref" >/dev/null
      digest="$(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$ref" | grep "^$(push_registry)/" | head -1 | sed 's/.*@//')"
      [[ "$digest" == sha256:* ]] || die "no digest recorded for $ref after push"
      echo "$digest" > "$E2E_STATE/images/$c.digest"
    done
    log "pushed ${#components[@]} image(s); digests in $E2E_STATE/images/"
    ;;
  save)
    mkdir -p "$dir"
    for c in "${components[@]}"; do
      ref="$(image_ref "$c")"
      docker image inspect "$ref" >/dev/null 2>&1 || die "$ref not found: run images.sh build first"
      log "saving $ref to $dir/$c.tar"
      docker save -o "$dir/$c.tar" "$ref"
    done
    ;;
  load)
    for tar in "$dir"/*.tar; do
      [[ -f "$tar" ]] || die "no images in $dir"
      log "loading $tar"
      docker load -q -i "$tar" >/dev/null
    done
    ;;
  *) echo "usage: $0 build|push|save <dir>|load <dir> [component...]" >&2; exit 1 ;;
esac
