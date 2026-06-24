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

# SPDX-License-Identifier: Apache-2.0
#
# Rewrites a git repository's history to move files to new locations.
# This is useful when merging multiple repos into a monorepo while preserving history.
#
# Usage:
#   ./rewrite-repo-paths.sh [options] <source-repo> <mapping-file> [output-dir]
#
# Options:
#   --drop-message-regex <regex>   Drop commits whose message matches this regex
#   --drop-author-regex <regex>    Drop commits whose author matches this regex
#   --drop-tags                    Remove all tags from the rewritten history
#   --help                         Show this help
#
# The mapping file contains lines of the form:
#   <old-path-prefix> <new-path-prefix>
#
# Example mapping file:
#   api/ apis/core/
#   internal/ operators/account-operator/internal/
#   cmd/ operators/account-operator/cmd/
#   pkg/ operators/account-operator/pkg/
#   main.go operators/account-operator/main.go
#
# Rules:
#   - Only files matching a mapping prefix are kept in history
#   - Files not matching any prefix are removed from history
#   - Paths are matched as prefixes
#   - Comments (lines starting with #) and empty lines are ignored
#
# Example with commit filtering:
#   ./rewrite-repo-paths.sh \
#       --drop-message-regex '^(chore\(deps\)|Update .* to )' \
#       --drop-author-regex 'renovate\[bot\]|dependabot' \
#       /path/to/source mapping.txt /tmp/output
#
# The script creates a fresh clone, rewrites it, and leaves it ready to be
# merged into the target repo with:
#   git remote add <name> <rewritten-repo-path>
#   git fetch <name>
#   git merge --allow-unrelated-histories <name>/main

set -euo pipefail

# Parse options
DROP_MESSAGE_REGEX=""
DROP_AUTHOR_REGEX=""
DROP_TAGS=false

show_help() {
    sed -n '2,/^$/p' "$0" | grep '^#' | sed 's/^# \?//'
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --drop-message-regex)
            DROP_MESSAGE_REGEX="$2"
            shift 2
            ;;
        --drop-author-regex)
            DROP_AUTHOR_REGEX="$2"
            shift 2
            ;;
        --drop-tags)
            DROP_TAGS=true
            shift
            ;;
        --help|-h)
            show_help
            ;;
        -*)
            echo "Unknown option: $1" >&2
            exit 1
            ;;
        *)
            break
            ;;
    esac
done

if [[ $# -lt 2 ]]; then
    echo "Usage: $0 [options] <source-repo> <mapping-file> [output-dir]" >&2
    echo "Run with --help for more information." >&2
    exit 1
fi

SOURCE_REPO="$1"
MAPPING_FILE="$2"
OUTPUT_DIR="${3:-/tmp/rewritten-repo}"

# Resolve to absolute paths
SOURCE_REPO="$(cd "$SOURCE_REPO" && pwd)"
MAPPING_FILE="$(realpath "$MAPPING_FILE")"

# Validate inputs
if [[ ! -d "$SOURCE_REPO/.git" ]] && [[ ! -d "$SOURCE_REPO/objects" ]]; then
    echo "Error: $SOURCE_REPO does not appear to be a git repository" >&2
    exit 1
fi

if [[ ! -f "$MAPPING_FILE" ]]; then
    echo "Error: Mapping file $MAPPING_FILE not found" >&2
    exit 1
fi

# Check for git filter-repo
if ! git filter-repo --version &>/dev/null; then
    echo "Error: git filter-repo is not installed" >&2
    echo "Install it with: sudo dnf install git-filter-repo" >&2
    exit 1
fi

# Clean up and create fresh clone
rm -rf "$OUTPUT_DIR"
echo "Cloning $SOURCE_REPO to $OUTPUT_DIR..."
git clone --no-local "$SOURCE_REPO" "$OUTPUT_DIR"

cd "$OUTPUT_DIR"

# Build git-filter-repo arguments
# We'll use a two-phase approach:
# 1. --path to keep only the files we care about
# 2. --path-rename to move them to new locations

PATH_ARGS=()
RENAME_ARGS=()

echo "Reading mappings from $MAPPING_FILE..."
while IFS= read -r line || [[ -n "$line" ]]; do
    # Skip empty lines and comments
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue

    # Parse old and new paths
    old_path=$(echo "$line" | awk '{print $1}')
    new_path=$(echo "$line" | awk '{print $2}')

    if [[ -z "$old_path" ]]; then
        continue
    fi

    echo "  $old_path -> ${new_path:-(delete)}"

    # Add --path to keep this prefix
    PATH_ARGS+=("--path" "$old_path")

    # Add --path-rename if we're moving it
    if [[ -n "$new_path" ]]; then
        RENAME_ARGS+=("--path-rename" "${old_path}:${new_path}")
    fi
done < "$MAPPING_FILE"

if [[ ${#PATH_ARGS[@]} -eq 0 ]]; then
    echo "Error: No valid mappings found in $MAPPING_FILE" >&2
    exit 1
fi

# Build commit callback if we need to filter commits
CALLBACK_FILE=""
if [[ -n "$DROP_MESSAGE_REGEX" || -n "$DROP_AUTHOR_REGEX" ]]; then
    CALLBACK_FILE=$(mktemp --suffix=.py)
    trap "rm -f '$CALLBACK_FILE'" EXIT

    cat > "$CALLBACK_FILE" << PYTHON_EOF
import re

# Compile regexes
message_pattern = re.compile(rb'''$DROP_MESSAGE_REGEX''') if '''$DROP_MESSAGE_REGEX''' else None
author_pattern = re.compile(rb'''$DROP_AUTHOR_REGEX''') if '''$DROP_AUTHOR_REGEX''' else None

dropped_count = 0

def commit_callback(commit, metadata):
    global dropped_count

    dominated = False

    # Check message
    if message_pattern and message_pattern.search(commit.message):
        dominated = True

    # Check author (name and email)
    if author_pattern:
        author_info = commit.author_name + b' <' + commit.author_email + b'>'
        if author_pattern.search(author_info):
            dominated = True

    if dominated:
        dropped_count += 1
        # Skip this commit entirely - its changes will be folded into the next commit
        commit.skip()
PYTHON_EOF

    echo ""
    echo "Commit filtering enabled:"
    [[ -n "$DROP_MESSAGE_REGEX" ]] && echo "  Drop messages matching: $DROP_MESSAGE_REGEX"
    [[ -n "$DROP_AUTHOR_REGEX" ]] && echo "  Drop authors matching: $DROP_AUTHOR_REGEX"
fi

echo ""
echo "Running git-filter-repo..."
echo "  Keeping ${#PATH_ARGS[@]} path prefixes"
echo "  Renaming ${#RENAME_ARGS[@]} paths"

# Build final command
FILTER_CMD=(git filter-repo --force "${PATH_ARGS[@]}" "${RENAME_ARGS[@]}")

if [[ "$DROP_TAGS" == "true" ]]; then
    # --tag-callback handles annotated tags
    FILTER_CMD+=(--tag-callback "tag.skip()")
fi

if [[ -n "$CALLBACK_FILE" ]]; then
    FILTER_CMD+=(--commit-callback "$(cat "$CALLBACK_FILE")")
fi

# Run it
"${FILTER_CMD[@]}"

# Delete all tags if requested (handles both annotated and lightweight tags)
if [[ "$DROP_TAGS" == "true" ]]; then
    echo "Removing all tags..."
    git tag -l | xargs -r git tag -d >/dev/null 2>&1 || true
fi

# Count commits before and after (approximately, by counting in log)
COMMIT_COUNT=$(git rev-list --count HEAD 2>/dev/null || echo "?")

echo ""
echo "Verifying result..."
echo "Commits in rewritten history: $COMMIT_COUNT"
echo ""
echo "Files in rewritten repo:"
git ls-files | head -20
FILE_COUNT=$(git ls-files | wc -l)
if [[ $FILE_COUNT -gt 20 ]]; then
    echo "... and $((FILE_COUNT - 20)) more files"
fi

echo ""
echo "Done! Rewritten repository is at: $OUTPUT_DIR"
echo ""
echo "To merge into your target repo:"
echo "  cd <target-repo>"
echo "  git remote add temp-merge $OUTPUT_DIR"
echo "  git fetch temp-merge"
echo "  git merge --allow-unrelated-histories temp-merge/<branch> -m 'Merge <name> with history'"
echo "  git remote remove temp-merge"
