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

# Prints the latest published Platform Mesh version: the highest final release
# (YY.MM.P, RFC 015; the registry also holds the 0.x versions from before) in
# the OCM repository, or the highest release candidate if there is no final
# release yet. Reads the tags of the aggregate's component descriptor in the
# registry anonymously, so it needs no GitHub CLI or token.
#
# Usage: latest-release.sh [<ocm-repo>]   (default ghcr.io/platform-mesh)

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

repo="${1:-$E2E_MIRROR_FROM}"
host="${repo%%/*}"; path="${repo#*/}"
descriptor="$path/component-descriptors/$E2E_COMPONENT"

command -v jq >/dev/null || die "jq is required"
token="$(curl -fsS "https://$host/token?scope=repository:$descriptor:pull" | jq -r '.token // empty')"
tags="$(curl -fsS ${token:+-H "Authorization: Bearer $token"} "https://$host/v2/$descriptor/tags/list?n=1000" | jq -r '.tags[]')"

calver='^[2-9][0-9]\.[0-9]+\.[0-9]+'   # YY.MM.P with YY >= 20, i.e. not the old 0.x
final="$(grep -E "${calver}\$" <<<"$tags" | sort -V | tail -1 || true)"
if [[ -n "$final" ]]; then echo "$final"; exit 0; fi
rc="$(grep -E "${calver}-rc\.[0-9]+\$" <<<"$tags" | sort -V | tail -1 || true)"
[[ -n "$rc" ]] || die "no Platform Mesh release (YY.MM.P) found in $repo"
echo "$rc"
