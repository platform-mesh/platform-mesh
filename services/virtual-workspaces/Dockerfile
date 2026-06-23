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
# The operator module is linked to the shared ./apis module through the
# repository-root go.work file, so the workspace and apis source must be
# present in the build:
#   docker build -f services/virtual-workspaces/Dockerfile .
FROM golang:1.26@sha256:87a41d2539e5671777734e91f467499ed5eafb1fb1f77221dff2744db7a51775 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Go workspace + the modules this image needs.
COPY go.work go.work
COPY go.work.sum go.work.sum
COPY apis/ apis/
COPY services/virtual-workspaces/ services/virtual-workspaces/

# Drop every workspace module except the ones copied above, so the workspace
# resolves with only apis + this operator — regardless of what else go.work lists
# (other operators, cmd/* tools, ...). This is a textual edit of go.work, so the
# dropped modules don't need to exist in the build context.
RUN for m in $(go work edit -json | sed -n 's/.*"DiskPath": "\(.*\)".*/\1/p'); do \
      case "$m" in \
        ./apis|./services/virtual-workspaces) ;; \
        *) go work edit -dropuse "$m" ;; \
      esac; \
    done

WORKDIR /workspace/services/virtual-workspaces

# cache deps before building so that source changes don't invalidate the layer
RUN go mod download

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /workspace/manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
