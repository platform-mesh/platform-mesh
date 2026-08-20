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

# Build the manager binary.
#
# NOTE: the build context is the repository ROOT, not this directory.
# The operator module is linked to the shared in-repo modules through the
# repository-root go.work file and the go.mod replace directives, so the
# workspace and those module sources must be present in the build:
#   docker build -f operators/backup-operator/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.26.4-bookworm@sha256:5f68ec6805843bd3981a951ffada82a26a0bd2631045c8f7dba483fa868f5ec5 AS builder
ARG TARGETARCH

WORKDIR /workspace

# Go workspace + the modules this image needs.
COPY go.work go.work
COPY go.work.sum go.work.sum
COPY apis/ apis/
COPY golang-commons/ golang-commons/
COPY subroutines/ subroutines/
COPY operators/backup-operator/ operators/backup-operator/

# Keep the copied workspace compatible with this module without changing the
# monorepo's root go.work file.
RUN go work edit -go=1.26.4

# Drop every workspace module except the ones copied above, so the workspace
# resolves with only the modules copied above — regardless of what else go.work lists
# (other operators, cmd/* tools, ...). This is a textual edit of go.work, so the
# dropped modules don't need to exist in the build context.
RUN for m in $(go work edit -json | sed -n 's/.*"DiskPath": "\(.*\)".*/\1/p'); do \
      case "$m" in \
        ./apis|./golang-commons|./subroutines|./operators/backup-operator) ;; \
        *) go work edit -dropuse "$m" ;; \
      esac; \
    done

WORKDIR /workspace/operators/backup-operator

# cache deps before building so that source changes don't invalidate the layer
RUN go mod download

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags '-w -s' -o /workspace/manager main.go

FROM scratch
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENV USER_UID=1001
ENV GROUP_UID=1001
COPY --from=builder --chown=${USER_UID}:${GROUP_UID}  /workspace/manager /operator/manager

USER ${USER_UID}:${GROUP_UID}
ENTRYPOINT ["/operator/manager"]
