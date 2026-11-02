#!/usr/bin/env bash

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

# The local OCI registry the snapshot is published to: a registry container on
# kind's docker network, reachable
#
#   - from the kind nodes, the ocm-k8s-toolkit and Flux by its IP on that
#     network (http://<ip>:5000, recorded in $E2E_STATE/registry.url and in
#     everything published), and
#   - from this machine on 127.0.0.1:<E2E_REGISTRY_HOST_PORT> for `docker push`.
#
# The IP is pinned (the second-to-last address of the network's IPv4 subnet,
# or E2E_REGISTRY_IP), since Docker hands out a new one on every restart and
# the address is baked into the published component descriptors. The content
# lives in the volume <E2E_REGISTRY_NAME>-data, so it survives the container;
# `registry.sh down` removes both.
#
# The ocm and helm CLIs on this machine talk to the IP as well, so it must be
# routable from the host: it is on Linux and with OrbStack; Docker Desktop does
# not route to containers. Then set E2E_REGISTRY_URL to an address that works
# everywhere (e.g. http://<hostname>:5000 with a hosts entry on this machine).
#
# Usage: registry.sh up|down|url

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

case "${1:-}" in
  up)
    mkdir -p "$E2E_STATE"
    if ! docker network inspect "$E2E_NETWORK" >/dev/null 2>&1; then
      # Created the way kind creates it (kind reuses an existing network of
      # that name), with explicit subnets: Docker only accepts a fixed --ip on
      # a network whose subnet was configured by the user.
      log "creating docker network $E2E_NETWORK ($E2E_NETWORK_SUBNET; kind reuses it)"
      docker network create -d bridge \
        -o com.docker.network.bridge.enable_ip_masquerade=true \
        -o com.docker.network.driver.mtu=1500 \
        --subnet "$E2E_NETWORK_SUBNET" \
        --ipv6 --subnet "$E2E_NETWORK_SUBNET6" \
        "$E2E_NETWORK" >/dev/null
    fi
    subnet="$(docker network inspect "$E2E_NETWORK" -f '{{range .IPAM.Config}}{{.Subnet}} {{end}}' | tr ' ' '\n' | grep -m1 '\.')"
    [[ -n "$subnet" ]] || die "network $E2E_NETWORK has no IPv4 subnet"
    ip="${E2E_REGISTRY_IP:-$(python3 -c 'import ipaddress,sys; print(list(ipaddress.ip_network(sys.argv[1]).hosts())[-2])' "$subnet")}"
    current="$(docker inspect -f "{{(index .NetworkSettings.Networks \"$E2E_NETWORK\").IPAddress}}" "$E2E_REGISTRY_NAME" 2>/dev/null || true)"
    if [[ "$(docker inspect -f '{{.State.Running}}' "$E2E_REGISTRY_NAME" 2>/dev/null)" != "true" || "$current" != "$ip" ]]; then
      docker rm -f "$E2E_REGISTRY_NAME" >/dev/null 2>&1 || true
      log "starting registry $E2E_REGISTRY_NAME ($E2E_REGISTRY_IMAGE) at $ip"
      run=(docker run -d --restart=always --name "$E2E_REGISTRY_NAME" --network "$E2E_NETWORK"
        -v "$E2E_REGISTRY_NAME-data:/var/lib/registry"
        -p "127.0.0.1:$E2E_REGISTRY_HOST_PORT:5000" "$E2E_REGISTRY_IMAGE")
      if ! "${run[@]:0:6}" --ip "$ip" "${run[@]:6}" >/dev/null 2>"$E2E_STATE/registry.err"; then
        # A network created elsewhere without an explicit subnet cannot pin
        # the address. Run without it; the address then changes when the
        # container is recreated, and the snapshot must be republished.
        log "cannot pin $ip on $E2E_NETWORK ($(tail -1 "$E2E_STATE/registry.err")); using a dynamic address"
        docker rm -f "$E2E_REGISTRY_NAME" >/dev/null 2>&1 || true
        "${run[@]}" >/dev/null
        ip="$(docker inspect -f "{{(index .NetworkSettings.Networks \"$E2E_NETWORK\").IPAddress}}" "$E2E_REGISTRY_NAME")"
      fi
    fi
    url="${E2E_REGISTRY_URL:-http://$ip:5000}"
    save_state registry.url "$url"
    for _ in 1 2 3 4 5; do curl -fsS "http://127.0.0.1:$E2E_REGISTRY_HOST_PORT/v2/" >/dev/null 2>&1 && break; sleep 1; done
    if curl -fsS --max-time 5 "$url/v2/" >/dev/null 2>&1; then
      log "registry at $url (push via 127.0.0.1:$E2E_REGISTRY_HOST_PORT)"
    else
      die "$url is not reachable from this machine; see the header of $0"
    fi
    ;;
  down)
    docker rm -f "$E2E_REGISTRY_NAME" >/dev/null 2>&1 && log "removed registry $E2E_REGISTRY_NAME" || true
    docker volume rm -f "$E2E_REGISTRY_NAME-data" >/dev/null 2>&1 || true
    rm -f "$E2E_STATE/registry.url"
    ;;
  url) registry_url ;;
  *) echo "usage: $0 up|down|url" >&2; exit 1 ;;
esac
