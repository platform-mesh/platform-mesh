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

# Build the platform-mesh-access-server and platform-mesh-access-controller
# binaries. Both ship in a single image; charts pick one via the container
# command.
#
# NOTE: the build context is the repository ROOT, not this directory.
# The sdk module is linked to shared in-repo modules through the
# repository-root go.work:
#   docker build -f sdk/access/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.27.3@sha256:2920bb124f1a2e12a1d39f0cab756ae0068441ee1da7655dec7f5e374944095b AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Stage 1: copy only the files that determine which modules get downloaded.
# This keeps the `go mod download` layer cached across source-only edits.
COPY go.work go.work.sum ./
COPY apis/go.mod apis/go.sum apis/
COPY protocol/go.mod protocol/go.sum protocol/
COPY sdk/go.mod sdk/go.sum sdk/

# Drop every workspace module except the ones copied above, so the workspace
# resolves with only the modules copied above — regardless of what else
# go.work lists. This is a textual edit of go.work, so the dropped modules
# don't need to exist in the build context.
RUN for m in $(go work edit -json | sed -n 's/.*"DiskPath": "\(.*\)".*/\1/p'); do \
      case "$m" in \
        ./apis|./protocol|./sdk) ;; \
        *) go work edit -dropuse "$m" ;; \
      esac; \
    done

WORKDIR /workspace/sdk

# Prime the module cache. The BuildKit cache mount persists downloaded
# modules across builds, so even when go.mod or go.sum changes, previously
# fetched modules are reused.
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Stage 2: copy the sources. This layer invalidates on source edits but
# does not re-run `go mod download`.
WORKDIR /workspace
COPY apis/ apis/
COPY protocol/ protocol/
COPY sdk/ sdk/

WORKDIR /workspace/sdk

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -ldflags '-w -s' -o /workspace/platform-mesh-access-server ./access/cmd/access-server && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -ldflags '-w -s' -o /workspace/platform-mesh-access-controller ./access/cmd/access-controller

FROM scratch
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENV USER_UID=1001
ENV GROUP_UID=1001
COPY --from=builder --chown=${USER_UID}:${GROUP_UID} /workspace/platform-mesh-access-server /sdk/platform-mesh-access-server
COPY --from=builder --chown=${USER_UID}:${GROUP_UID} /workspace/platform-mesh-access-controller /sdk/platform-mesh-access-controller

USER ${USER_UID}:${GROUP_UID}
# No default ENTRYPOINT — the chart (server vs. controller) sets command.
