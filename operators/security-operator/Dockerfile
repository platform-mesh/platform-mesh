# Build the manager binary.
#
# NOTE: the build context is the repository ROOT, not this directory.
# The operator module depends on the shared ./apis module via a relative
# replace (../../apis), so the apis module source must be present in the build
# at the matching layout.
#   docker build -f operators/security-operator/Dockerfile .
FROM golang:1.26@sha256:87a41d2539e5671777734e91f467499ed5eafb1fb1f77221dff2744db7a51775 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# The shared apis module and this operator's module, in their repo-relative layout
# so the `replace => ../../apis` directive resolves.
COPY apis/ apis/
COPY operators/security-operator/ operators/security-operator/

WORKDIR /workspace/operators/security-operator

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
