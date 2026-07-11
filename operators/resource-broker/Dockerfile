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

# NOTE: the build context is the repository ROOT, not this directory.
# The operator module is linked to the shared ./apis module through the
# repository-root go.work file, so the workspace and apis source must be
# present in the build:
#   docker build -f operators/resource-broker/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.26@sha256:792443b89f65105abba56b9bd5e97f680a80074ac62fc844a584212f8c8102c3 AS builder

WORKDIR /workspace

# Go workspace + the modules this image needs.
COPY go.work go.work
COPY go.work.sum go.work.sum
COPY apis/ apis/
COPY operators/resource-broker/ operators/resource-broker/

# Drop every workspace module except the ones copied above, so the workspace
# resolves with only apis + this operator — regardless of what else go.work lists
# (other operators, cmd/* tools, ...). This is a textual edit of go.work, so the
# dropped modules don't need to exist in the build context.
RUN for m in $(go work edit -json | sed -n 's/.*"DiskPath": "\(.*\)".*/\1/p'); do \
      case "$m" in \
        ./apis|./operators/resource-broker) ;; \
        *) go work edit -dropuse "$m" ;; \
      esac; \
    done

WORKDIR /workspace/operators/resource-broker

# cache deps before building so that source changes don't invalidate the layer
RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-broker go mod download

RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-broker \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-broker \
    CGO_ENABLED=0 \
    GOCACHE=/root/.cache/go-build \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build -ldflags '-w -s' -o /workspace/manager .

FROM scratch
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENV USER_UID=1001
ENV GROUP_UID=1001
COPY --from=builder --chown=${USER_UID}:${GROUP_UID}  /workspace/manager /operator/manager

USER ${USER_UID}:${GROUP_UID}
ENTRYPOINT ["/operator/manager"]
