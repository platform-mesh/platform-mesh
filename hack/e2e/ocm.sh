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

# Publishes the snapshot's OCM component graph to the local registry, with the
# release scripts: an image component per pushed image
# (hack/release/build-image-component.sh), then the charts, their components,
# the third-party wrappers and the aggregate (hack/release/build-aggregate.sh).
# Components from other repositories (portal, iam-ui, ...) are copied from
# E2E_MIRROR_FROM. Everything is signed with a throwaway key generated here;
# install.sh hands its certificate to the cluster.
#
# Usage: ocm.sh [--bump]
#   --bump   publish the charts and aggregate as a new version on the same
#            images (after a chart change; see version.sh)
#
# Writes the ocm config to $E2E_STATE/ocmconfig; set OCM_CONFIG to it to use
# the ocm CLI against the registry yourself.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
require_tools ocm helm yq

[[ "${1:-}" == "--bump" ]] && "$E2E_ROOT/hack/e2e/version.sh" --bump >/dev/null

image_version="$(image_version)"
version="$(snapshot_version)"
ocm_repo="$(ocm_repo)"
registry_host="$(registry_host)"
commit="$(git -C "$E2E_ROOT" rev-parse HEAD)"

# Throwaway signing key; the signature name must equal the certificate's CN.
if [[ ! -f "$E2E_STATE/signing.key" ]]; then
  log "generating signing key for signature $E2E_SIGNATURE"
  openssl req -x509 -newkey rsa:3072 -nodes -days 30 -subj "/CN=$E2E_SIGNATURE" \
    -keyout "$E2E_STATE/signing.key" -out "$E2E_STATE/signing.crt" 2>/dev/null
fi
# Registry credentials (ghcr.io, when GITHUB_TOKEN is set, against rate limits)
# and the signing key. The local registry needs no credentials.
OCM_SIGNING_PRIVATE_KEY="$(cat "$E2E_STATE/signing.key")" OCM_SIGNING_CERT="$(cat "$E2E_STATE/signing.crt")" \
  "$E2E_ROOT/hack/release/ocm-config.sh" "$E2E_MIRROR_FROM" "$E2E_SIGNATURE" > "$E2E_STATE/ocmconfig"
export OCM_CONFIG="$E2E_STATE/ocmconfig"

# Image components: one per image pushed by images.sh.
for c in $(image_components); do
  [[ -f "$E2E_STATE/images/$c.digest" ]] || die "no digest for $c: run images.sh push (task e2e:images) first"
  name="github.com/platform-mesh/images/$c"
  if "$OCM" get component-version "$ocm_repo//$name:$image_version" -o json >/dev/null 2>&1; then
    log "$name:$image_version already published"
    continue
  fi
  "$E2E_ROOT/hack/release/build-image-component.sh" \
    --component "$c" --version "$image_version" \
    --image-ref "http://$registry_host/$IMAGE_REPO/$c@$(cat "$E2E_STATE/images/$c.digest")" \
    --ocm-repo "$ocm_repo" --signature "$E2E_SIGNATURE" --repo-url "$E2E_REPO_URL" --commit "$commit"
done

# Charts, chart/service components, third-party wrappers, aggregate.
log "publishing $E2E_COMPONENT:$version (images v$image_version) to $ocm_repo"
"$E2E_ROOT/hack/release/build-aggregate.sh" \
  --version "$version" --image-version "$image_version" \
  --ocm-repo "$ocm_repo" --chart-repo "$(chart_repo)" \
  --mirror-from "$E2E_MIRROR_FROM" --signature "$E2E_SIGNATURE" \
  --repo-url "$E2E_REPO_URL" --commit "$commit"
save_state version "$version"
log "published $E2E_COMPONENT:$version"
