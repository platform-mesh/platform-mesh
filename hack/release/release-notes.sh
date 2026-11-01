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

# Generates the release notes for a Platform Mesh release from this
# repository's history, written to stdout as Markdown.
#
# Usage: release-notes.sh <tag> [<previous-tag>]
#
# The previous tag defaults to the highest release tag below <tag>; for a
# final release only final releases are considered, for a release candidate
# any release tag is.
#
# Sections:
#   - third-party versions that changed (ocm/versions.yaml)
#   - versions of the components built in other repositories that changed
#     (appVersion of their charts)
#   - merged pull requests between the two tags, grouped by the part of the
#     repository they touched
#
# Everything built from this repository has the release's version, so there is
# no per-component version table.
#
# Needs the full git history (fetch-depth: 0 in workflows). Uses `gh` only to
# build links; works without it.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

tag="${1:?usage: $0 <tag> [<previous-tag>]}"
prev="${2:-}"
repo_url="$(git remote get-url origin 2>/dev/null | sed -E 's#^git@github.com:#https://github.com/#; s#\.git$##')"

release_tags() { git tag -l 'v[0-9]*' | sort -V; }

if [[ -z "$prev" ]]; then
  if [[ "$tag" == *-* ]]; then
    candidates="$(release_tags)"
  else
    candidates="$(release_tags | grep -v -- '-')"
  fi
  prev="$(printf '%s\n%s\n' "$candidates" "$tag" | sort -V | uniq | grep -B1 -x "$tag" | head -1)"
  [[ "$prev" == "$tag" ]] && prev=""
fi

version="${tag#v}"
if [[ -n "$prev" ]]; then
  echo "Changes since [${prev#v}]($repo_url/releases/tag/$prev). Full diff: [\`$prev...$tag\`]($repo_url/compare/$prev...$tag)."
else
  echo "Initial release."
fi
echo

# ---------------------------------------------------------------------------
# Components from other repositories (portal, iam-ui, ...)
# ---------------------------------------------------------------------------
chart_field() { # <ref> <chart> <field>
  git show "$1:charts/$2/Chart.yaml" 2>/dev/null | yq -r ".$3 // \"\"" 2>/dev/null || true
}

if [[ -n "$prev" ]]; then
  inhouse="$(git show "$tag:ocm/components.yaml" | yq '.components[] | select(has("chart")) | .chart' | sed 's#^charts/##')"
  rows=""
  for chart_yaml in charts/*/Chart.yaml; do
    name="$(basename "$(dirname "$chart_yaml")")"
    grep -qx "$name" <<<"$inhouse" && continue
    av="$(chart_field "$tag" "$name" appVersion)"; pav="$(chart_field "$prev" "$name" appVersion)"
    [[ -n "$av" && "$av" != "0.0.0" && "$av" != "$pav" ]] || continue
    rows+="| $name | ${pav:-–} | **$av** |"$'\n'
  done
  if [[ -n "$rows" ]]; then
    echo "## Components from other repositories"
    echo
    echo "| Component | Before | After |"
    echo "|---|---|---|"
    printf '%s' "$rows"
    echo
  fi
fi

# ---------------------------------------------------------------------------
# Third-party versions
# ---------------------------------------------------------------------------
if [[ -n "$prev" ]] && git cat-file -e "$prev:ocm/versions.yaml" 2>/dev/null; then
  changes="$(diff \
    <(git show "$prev:ocm/versions.yaml" | yq -r 'to_entries | .[] | select(.key | test("^[A-Z]")) | .key + " " + .value') \
    <(git show "$tag:ocm/versions.yaml"  | yq -r 'to_entries | .[] | select(.key | test("^[A-Z]")) | .key + " " + .value') \
    | grep -E '^[<>]' || true)"
  if [[ -n "$changes" ]]; then
    echo "## Third-party versions"
    echo
    echo "| Setting | Before | After |"
    echo "|---|---|---|"
    while read -r key; do
      before="$(grep "^< $key " <<<"$changes" | awk '{print $3}')"
      after="$(grep "^> $key " <<<"$changes" | awk '{print $3}')"
      echo "| \`$key\` | ${before:-–} | ${after:-–} |"
    done < <(awk '{print $2}' <<<"$changes" | sort -u)
    echo
  fi
fi

# ---------------------------------------------------------------------------
# Pull requests, grouped by area
# ---------------------------------------------------------------------------
area_of() { # <path> -> area label
  case "$1" in
    operators/*|services/*) echo "$1" | cut -d/ -f2 ;;
    apis/*|golang-commons/*|subroutines/*) echo "$1" | cut -d/ -f1 ;;
    charts/*/*) echo "chart: $(echo "$1" | cut -d/ -f2)" ;;
    charts/*) echo "repository" ;;
    installation/*|tests/*) echo "$1" | cut -d/ -f1 ;;
    ocm/*|.github/*|hack/*|Taskfile.yaml|docs/*) echo "repository" ;;
    *) echo "other" ;;
  esac
}

if [[ -n "$prev" ]]; then
  declare -A by_area
  # First-parent history is one entry per merged PR (or squash commit).
  while IFS=$'\t' read -r sha subject; do
    pr=""
    if [[ "$subject" =~ ^Merge\ pull\ request\ \#([0-9]+) ]]; then
      pr="${BASH_REMATCH[1]}"
      # Use the PR's title from the merge commit body (second paragraph).
      title="$(git log -1 --format=%b "$sha" | sed -n '1p')"
    elif [[ "$subject" =~ \(#([0-9]+)\)$ ]]; then
      pr="${BASH_REMATCH[1]}"
      title="${subject% (#*}"
    else
      title="$subject"
    fi
    [[ -n "$title" ]] || title="$subject"
    link=""; [[ -n "$pr" ]] && link=" ([#$pr]($repo_url/pull/$pr))"

    areas="$(git diff-tree --no-commit-id --name-only -r -m --first-parent "$sha" | while read -r f; do area_of "$f"; done | sort -u)"
    [[ -n "$areas" ]] || areas="other"
    # Cross-cutting changes (dependency bumps, boilerplate, ...) are listed once.
    if [[ "$(wc -l <<<"$areas")" -gt 3 ]]; then
      areas="multiple areas"
    fi
    while read -r area; do
      by_area["$area"]="${by_area[$area]-}- ${title}${link}"$'\n'
    done <<<"$areas"
  done < <(git log --first-parent --format='%H%x09%s' "$prev..$tag")

  if [[ ${#by_area[@]} -gt 0 ]]; then
    echo "## Changes"
    echo
    while IFS= read -r area; do
      echo "### $area"
      echo
      printf '%s' "${by_area[$area]}"
      echo
    done < <(printf '%s\n' "${!by_area[@]}" | sort)
  fi
fi
