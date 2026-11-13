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

# Build the extension-manager-operator binary.
#
# NOTE: the build context is the repository ROOT, not this directory.
# The operator module is linked to the shared in-repo modules through the
# go.mod replace directives, so those module sources must be present in the build:
#   docker build -f operators/extension-manager-operator/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.27.3@sha256:2920bb124f1a2e12a1d39f0cab756ae0068441ee1da7655dec7f5e374944095b AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Local modules used by go.mod replace directives.
COPY apis/ apis/
COPY golang-commons/ golang-commons/
COPY subroutines/ subroutines/
COPY operators/extension-manager-operator/ operators/extension-manager-operator/

WORKDIR /workspace/operators/extension-manager-operator

# cache deps before building so that source changes don't invalidate the layer
RUN go mod download

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags '-w -s' -o /workspace/extension-manager-operator main.go

FROM scratch
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENV USER_UID=1001
ENV GROUP_UID=1001
COPY --from=builder --chown=${USER_UID}:${GROUP_UID}  /workspace/extension-manager-operator /operator/extension-manager-operator

USER ${USER_UID}:${GROUP_UID}
ENTRYPOINT ["/operator/extension-manager-operator"]
