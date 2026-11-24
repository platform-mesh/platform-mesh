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

# Build the access virtual workspace binaries.
#
# NOTE: the build context is the repository ROOT, not this directory, like for
# every image of this repository:
#   docker build -f operators/account-operator/virtual-workspace/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.27.3@sha256:2920bb124f1a2e12a1d39f0cab756ae0068441ee1da7655dec7f5e374944095b AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace/operators/account-operator/virtual-workspace

# cache deps before building so that source changes don't invalidate the layer
COPY operators/account-operator/virtual-workspace/go.mod operators/account-operator/virtual-workspace/go.sum ./
RUN go mod download

COPY operators/account-operator/virtual-workspace/ ./

# The server, the bootstrap run as its init container (provider rbac), and a
# client turning a review into a kubeconfig. GOARCH has no default so that the
# binaries match the platform the image is built for.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags '-w -s' -o /workspace/bin/access-vw ./cmd/access-vw \
    && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags '-w -s' -o /workspace/bin/access-vw-init ./cmd/init \
    && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags '-w -s' -o /workspace/bin/scar-to-kubeconfig ./cmd/scar-to-kubeconfig

# Use distroless as minimal base image to package the marketplace binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
WORKDIR /
# At the root of the image: kcp-operator starts them by path (command of the
# VirtualWorkspace, see charts/access-vw).
COPY --from=builder /workspace/bin/access-vw /workspace/bin/access-vw-init /workspace/bin/scar-to-kubeconfig /
USER 65532:65532

ENTRYPOINT ["/access-vw"]
