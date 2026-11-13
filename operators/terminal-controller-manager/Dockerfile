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

# Build the terminal-controller-manager binary.
#
# NOTE: the build context is the repository ROOT, not this directory.
# The operator module is linked to the shared in-repo modules through the
# go.mod replace directives, so those module sources must be present in the build:
#   docker build -f operators/terminal-controller-manager/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.27.3@sha256:2920bb124f1a2e12a1d39f0cab756ae0068441ee1da7655dec7f5e374944095b AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Local modules used by go.mod replace directives.
COPY apis/ apis/
COPY golang-commons/ golang-commons/
COPY subroutines/ subroutines/
COPY operators/terminal-controller-manager/ operators/terminal-controller-manager/

WORKDIR /workspace/operators/terminal-controller-manager

# cache deps before building so that source changes don't invalidate the layer
RUN go mod download

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags '-w -s' -o /workspace/terminal-controller-manager main.go

# Use distroless as minimal base image to package the terminal-controller-manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
WORKDIR /
COPY --from=builder /workspace/terminal-controller-manager /app/terminal-controller-manager
USER 65532:65532

ENTRYPOINT ["/app/terminal-controller-manager"]
