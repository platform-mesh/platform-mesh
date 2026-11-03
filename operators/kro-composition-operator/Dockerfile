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
# The module is linked through the repository-root go.work; this operator depends
# only on external modules (kro, kcp SDK, controller-runtime), not the shared ./apis.
FROM --platform=$BUILDPLATFORM golang:1.27.3@sha256:2920bb124f1a2e12a1d39f0cab756ae0068441ee1da7655dec7f5e374944095b AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.work go.work
COPY go.work.sum go.work.sum
COPY operators/kro-composition-operator/ operators/kro-composition-operator/

# Drop every workspace module except this operator so the workspace resolves with
# only the copied source, regardless of what else go.work lists.
RUN for m in $(go work edit -json | sed -n 's/.*"DiskPath": "\(.*\)".*/\1/p'); do \
      case "$m" in \
        ./operators/kro-composition-operator) ;; \
        *) go work edit -dropuse "$m" ;; \
      esac; \
    done

WORKDIR /workspace/operators/kro-composition-operator

RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-kco go mod download

RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-kco \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-kco \
    CGO_ENABLED=0 \
    GOCACHE=/root/.cache/go-build \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH} \
    go build -ldflags '-w -s' -o /workspace/manager ./cmd

FROM scratch
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENV USER_UID=1001
ENV GROUP_UID=1001
COPY --from=builder --chown=${USER_UID}:${GROUP_UID} /workspace/manager /operator/manager

USER ${USER_UID}:${GROUP_UID}
ENTRYPOINT ["/operator/manager"]
