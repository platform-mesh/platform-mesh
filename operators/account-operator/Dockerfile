# Build the manager binary.
#
# NOTE: the build context is the repository ROOT, not this directory.
# The operator module depends on the shared ./apis module via a relative
# replace (../../apis), so the apis module source must be present in the build
# at the matching layout.
#   docker build -f operators/account-operator/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.26.4-bookworm@sha256:5f68ec6805843bd3981a951ffada82a26a0bd2631045c8f7dba483fa868f5ec5 AS builder
ARG TARGETARCH

WORKDIR /workspace

# The shared apis module and this operator's module, in their repo-relative layout
# so the `replace => ../../apis` directive resolves.
COPY apis/ apis/
COPY operators/account-operator/ operators/account-operator/

WORKDIR /workspace/operators/account-operator

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