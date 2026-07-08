# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.26.4-trixie@sha256:a425cb16126d04e54977fca468d65d3f5af8722f8476cf2fe48906d47a626ec0 AS builder
ARG TARGETARCH

WORKDIR /workspace

# Set the Go module proxy to use direct fetching, which may bypass the DNS timeout issues.
ENV GOPROXY=direct

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/
COPY pkg/ pkg/
COPY manifests/ manifests/

COPY gotemplates/ gotemplates/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags '-w -s' -o manager main.go

FROM scratch
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENV USER_UID=1001
ENV GROUP_UID=1001
COPY --from=builder --chown=${USER_UID}:${GROUP_UID} /workspace/manager /operator/manager
COPY --from=builder --chown=${USER_UID}:${GROUP_UID} /workspace/manifests /operator/manifests
COPY --from=builder --chown=${USER_UID}:${GROUP_UID} /workspace/gotemplates /operator/gotemplates
USER ${USER_UID}:${GROUP_UID}
ENTRYPOINT ["/operator/manager"]