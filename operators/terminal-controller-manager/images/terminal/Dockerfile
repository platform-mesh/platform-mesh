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

# Build stage for k9s (nexus49 fork with kcp support)
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS k9s-builder

ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache git make

# Enable GOTOOLCHAIN to auto-download required Go version
ENV GOTOOLCHAIN=auto

RUN git clone --depth 1 -b feat/kcp-support https://github.com/nexus49/k9s.git /tmp/k9s && \
    cd /tmp/k9s && \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} make build

# Runtime stage
FROM alpine:3.20

ARG TARGETARCH

# Install required tools including ttyd for WebSocket terminal
RUN apk add --no-cache \
    bash \
    curl \
    jq \
    vim \
    ca-certificates \
    ttyd

# Install kubectl
RUN KUBECTL_VERSION=$(curl -Ls https://dl.k8s.io/release/stable.txt) && \
    curl -LO "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" && \
    chmod +x kubectl && \
    mv kubectl /usr/local/bin/

# Install kcp kubectl plugins (kcp, ws)
RUN KCP_VERSION=$(curl -s https://api.github.com/repos/kcp-dev/kcp/releases/latest | jq -r '.tag_name') && \
    curl -LO "https://github.com/kcp-dev/kcp/releases/download/${KCP_VERSION}/kubectl-kcp-plugin_${KCP_VERSION#v}_linux_${TARGETARCH}.tar.gz" && \
    curl -LO "https://github.com/kcp-dev/kcp/releases/download/${KCP_VERSION}/kubectl-ws-plugin_${KCP_VERSION#v}_linux_${TARGETARCH}.tar.gz" && \
    tar -xzf "kubectl-kcp-plugin_${KCP_VERSION#v}_linux_${TARGETARCH}.tar.gz" -C /tmp && \
    tar -xzf "kubectl-ws-plugin_${KCP_VERSION#v}_linux_${TARGETARCH}.tar.gz" -C /tmp && \
    mv /tmp/bin/kubectl-kcp /tmp/bin/kubectl-ws /usr/local/bin/ && \
    rm -rf "kubectl-kcp-plugin_${KCP_VERSION#v}_linux_${TARGETARCH}.tar.gz" "kubectl-ws-plugin_${KCP_VERSION#v}_linux_${TARGETARCH}.tar.gz" /tmp/bin

# Copy k9s from builder
COPY --from=k9s-builder /tmp/k9s/execs/k9s /usr/local/bin/k9s

# Create non-root user
RUN adduser -D -u 1000 terminal

# Set up shell aliases in /etc/profile.d (not affected by emptyDir mount on /home/terminal)
RUN echo 'alias k=kubectl' > /etc/profile.d/kubectl.sh && \
    echo 'source <(kubectl completion bash)' >> /etc/profile.d/kubectl.sh && \
    echo 'complete -o default -F __start_kubectl k' >> /etc/profile.d/kubectl.sh

USER terminal
WORKDIR /home/terminal

# Copy scripts to /usr/local/bin
COPY setup.sh /usr/local/bin/setup.sh
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
USER root
RUN chmod +x /usr/local/bin/setup.sh /usr/local/bin/entrypoint.sh
USER terminal

# Set kubeconfig location (tmpfs will be mounted at runtime)
ENV KUBECONFIG=/tmp/kubeconfig
# Enable kcp support in k9s (nexus49 fork feature)
ENV K9S_FEATURE_GATE_KCP_SUPPORT=true

# Expose ttyd port
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
