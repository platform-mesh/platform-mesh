#!/usr/bin/env bash

set -euo pipefail
cd $(dirname $0)

rm -rf source rewritten
git clone https://github.com/platform-mesh/account-operator source

../rewrite-repo-paths.sh \
  --drop-author-regex 'renovate\[bot\]' \
  --drop-tags \
  source \
  mappings.txt \
  rewritten
