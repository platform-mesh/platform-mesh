#!/usr/bin/env sh

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

# Run Renovate against a throwaway local Forgejo container and fetch the
# resulting deps/* branches into this repository. Nothing touches GitHub.
#
# Subcommands: up, push, run, fetch, apply, down. No argument runs the
# up-push-run-fetch pipeline and tears the container down afterwards. A failed
# stage leaves the container running for inspection; clean up with
# `deps-local.sh down`.
#
# Env overrides:
#   FORGEJO_IMAGE   forgejo container image
#   FORGEJO_PORT    host port for the Forgejo HTTP API (127.0.0.1 only)
#   RENOVATE_IMAGE  renovate container image

log() { echo ">>> $@"; }
die() { echo "$@" >&2; exit 1; }

FORGEJO_IMAGE="${FORGEJO_IMAGE:-code.forgejo.org/forgejo/forgejo:13}"
FORGEJO_PORT="${FORGEJO_PORT:-3300}"
RENOVATE_IMAGE="${RENOVATE_IMAGE:-ghcr.io/renovatebot/renovate:44.202.1}"

NET=deps-local
CONTAINER=deps-local-forgejo
REPO=renovate/platform-mesh
USER=renovate
API="http://127.0.0.1:${FORGEJO_PORT}/api/v1"

command -v docker >/dev/null 2>&1 || die "docker not found"
git rev-parse --git-dir >/dev/null 2>&1 || die "not inside a git repository"

_token() {
    docker exec -u git "$CONTAINER" forgejo admin user generate-access-token \
        --username "$USER" \
        --token-name "deps-local-$(date +%s)" \
        --scopes write:repository,write:user,write:issue,read:organization \
        --raw \
        || die "token generation failed"
}

_giturl() {
    echo "http://${USER}:$(_token)@127.0.0.1:${FORGEJO_PORT}/${REPO}.git"
}

# Current working tree + changes in a temporary branch.
_snapshot() {
    tmpidx="$(mktemp)" || die "mktemp failed"
    GIT_INDEX_FILE="$tmpidx" git read-tree HEAD || die "read-tree failed"
    GIT_INDEX_FILE="$tmpidx" git add -A . || die "snapshot add failed"
    tree=$(GIT_INDEX_FILE="$tmpidx" git write-tree) || die "write-tree failed"
    rm -f "$tmpidx"
    git commit-tree "$tree" -p HEAD -m "deps-local snapshot" || die "commit-tree failed"
}

# Start forgejo
_up() {
    docker network inspect "$NET" >/dev/null 2>&1 \
        || docker network create "$NET" >/dev/null \
        || die "network create failed"
    # ROOT_URL uses the container name so clone URLs the API hands to
    # Renovate resolve inside the docker network.
    docker run -d --name "$CONTAINER" --network "$NET" \
        -p "127.0.0.1:${FORGEJO_PORT}:3000" \
        -e FORGEJO__security__INSTALL_LOCK=true \
        -e FORGEJO__server__ROOT_URL="http://${CONTAINER}:3000/" \
        "$FORGEJO_IMAGE" >/dev/null \
        || die "forgejo start failed"

    log "waiting for forgejo"
    i=0
    until curl -fsS "http://127.0.0.1:${FORGEJO_PORT}/api/healthz" >/dev/null 2>&1; do
        i=$((i + 1))
        test "$i" -ge 60 && die "forgejo not healthy after 60s"
        sleep 1
    done

    pw=$(tr -dc A-Za-z0-9 </dev/urandom | head -c 20)
    docker exec -u git "$CONTAINER" forgejo admin user create \
        --admin \
        --username "$USER" \
        --password "$pw" \
        --email renovate@localhost.localdomain \
        --must-change-password=false >/dev/null \
        || die "user create failed"
    log "forgejo up at http://127.0.0.1:${FORGEJO_PORT}"
}

# Push the snapshot to forgejo.
_push() {
    token=$(_token)
    curl -fsS -X POST "${API}/user/repos" \
        -H "Authorization: token $token" \
        -H "Content-Type: application/json" \
        -d "{\"name\": \"$(basename "$REPO")\", \"auto_init\": false}" >/dev/null \
        || die "repo create failed"
    log "pushing working tree snapshot"
    git push --quiet "http://${USER}:${token}@127.0.0.1:${FORGEJO_PORT}/${REPO}.git" \
        "$(_snapshot):refs/heads/main" \
        || die "push failed"
}

# Run renovate against the running forgejo instance.
_run() {
    log "running renovate ($RENOVATE_IMAGE)"
    # The github.com token is used for datasource ghcr.io lookups.
    # Without it requests tend to fail.
    ghtoken=$(gh auth token 2>/dev/null) || log "warning: no gh token, github datasources will be rate-limited"
    docker run --rm --network "$NET" \
        -e GITHUB_COM_TOKEN="$ghtoken" \
        -e RENOVATE_TOKEN="$(_token)" \
        -e RENOVATE_CONFIG_FILE=/cli-local.json \
        -e LOG_LEVEL="${LOG_LEVEL:-info}" \
        -v "$(git rev-parse --show-toplevel)/.renovate/cli-local.json:/cli-local.json:ro" \
        "$RENOVATE_IMAGE" renovate \
        || die "renovate failed"
}

# Fetch the branch resulting from the renovate run.
_fetch() {
    log "fetching deps/* branches"
    git fetch --no-prune "$(_giturl)" "+refs/heads/deps/*:refs/heads/deps/*"
    git branch --list 'deps/*'
}

# Apply the changes renovate made to the working tree.
_apply() {
    git rev-parse --verify --quiet refs/heads/deps/all >/dev/null \
        || die "no deps/all branch; run the update first"
    if git diff --quiet deps/all~1 deps/all; then
        log "deps/all is empty, nothing to apply"
        return
    fi
    git diff deps/all~1 deps/all | git apply || die "apply failed"
    log "applied deps/all to the working tree"
}

# Shutdown forgejo.
_down() {
    docker rm -f "$CONTAINER" >/dev/null 2>&1
    docker network rm "$NET" >/dev/null 2>&1
    log "cleaned up"
}

case "${1:-all}" in
    (up) _up;;
    (push) _push;;
    (run) _run;;
    (fetch) _fetch;;
    (apply) _apply;;
    (down) _down;;
    (all)
        _up
        _push
        _run
        _fetch
        _down
        ;;
    (*) die "Unknown subcommand: $1 (up|push|run|fetch|apply|down)";;
esac
