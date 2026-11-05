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

# Prints a DOCKER_HOST value suitable for Tilt's docker_build. Empty output
# means "use the default" (docker socket). Rules:
#
#   1. If DOCKER_HOST is already set, echo it unchanged.
#   2. If `docker info` works, print nothing (let Tilt use its default).
#   3. If `podman info` works, print the podman user socket as a unix://
#      URL (via `podman info --format '{{.Host.RemoteSocket.Path}}'`).
#   4. Otherwise print nothing and emit a diagnostic to stderr.
#
# Usage from a Taskfile:
#   env:
#     DOCKER_HOST:
#       sh: contrib/tilt/hack/docker-host.sh

set -u

installed() { command -v "$1" > /dev/null 2>&1; }

if [ -n "${DOCKER_HOST:-}" ]; then
    printf '%s\n' "$DOCKER_HOST"
    exit 0
fi

if installed docker && docker info > /dev/null 2>&1; then
    # Default docker — leave DOCKER_HOST unset.
    exit 0
fi

if installed podman && podman info > /dev/null 2>&1; then
    socket="$(podman info --format '{{.Host.RemoteSocket.Path}}' 2>/dev/null)" || socket=""
    if [ -z "$socket" ]; then
        echo "docker-host.sh: podman detected but did not report a RemoteSocket.Path — upgrade podman or set DOCKER_HOST manually." >&2
        exit 1
    fi
    if [ ! -S "$socket" ]; then
        # podman reports a path even when the systemd user socket unit is not
        # running. Tilt's docker_build needs the socket; fail now with an
        # actionable error rather than letting Tilt fail later.
        {
            echo "docker-host.sh: podman detected but socket $socket does not exist."
            echo "Start it with:"
            echo "    systemctl --user enable --now podman.socket"
            echo "then re-run this task."
        } >&2
        exit 1
    fi
    printf 'unix://%s\n' "$socket"
    exit 0
fi

echo "docker-host.sh: neither docker nor podman is usable — Tilt's docker_build will fail." >&2
exit 1
