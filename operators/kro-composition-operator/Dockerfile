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

# NOTE: the build context is the repository ROOT, not this directory:
#   docker build -f operators/kro-composition-operator/Dockerfile .
# This operator depends only on external modules (kro, kcp SDK,
# controller-runtime), not the shared ./apis.
FROM --platform=$BUILDPLATFORM golang:1.27.3@sha256:2920bb124f1a2e12a1d39f0cab756ae0068441ee1da7655dec7f5e374944095b AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY operators/kro-composition-operator/ operators/kro-composition-operator/

WORKDIR /workspace/operators/kro-composition-operator

RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-kco go mod download

RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-kco \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-kco \
    CGO_ENABLED=0 \
    GOCACHE=/root/.cache/go-build \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build -ldflags '-w -s' -o /workspace/kro-composition-operator ./cmd

FROM scratch
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENV USER_UID=1001
ENV GROUP_UID=1001
COPY --from=builder --chown=${USER_UID}:${GROUP_UID} /workspace/kro-composition-operator /operator/kro-composition-operator

USER ${USER_UID}:${GROUP_UID}
ENTRYPOINT ["/operator/kro-composition-operator"]
