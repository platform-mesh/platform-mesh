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
# The operator module is linked to the shared in-repo modules through the
# go.mod replace directives, so those module sources must be present in the build:
#   docker build -f operators/resource-broker/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.27.3@sha256:2920bb124f1a2e12a1d39f0cab756ae0068441ee1da7655dec7f5e374944095b AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Local modules used by go.mod replace directives.
COPY apis/ apis/
COPY golang-commons/ golang-commons/
COPY subroutines/ subroutines/
COPY operators/resource-broker/ operators/resource-broker/

WORKDIR /workspace/operators/resource-broker

# cache deps before building so that source changes don't invalidate the layer
RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-broker go mod download

RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-broker \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-broker \
    CGO_ENABLED=0 \
    GOCACHE=/root/.cache/go-build \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build -ldflags '-w -s' -o /workspace/resource-broker .

FROM scratch
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENV USER_UID=1001
ENV GROUP_UID=1001
COPY --from=builder --chown=${USER_UID}:${GROUP_UID}  /workspace/resource-broker /operator/resource-broker

USER ${USER_UID}:${GROUP_UID}
ENTRYPOINT ["/operator/resource-broker"]
