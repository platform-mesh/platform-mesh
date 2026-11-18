#!/usr/bin/env sh

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

# Makes this machine trust the CA of the Tilt environment, or stops it.
#
#   trust-ca.sh trust|untrust
#
# The gateway serves the portal, Keycloak and Dex with a certificate of a CA
# that cert-manager creates in the kind cluster. Tools that take no CA of
# their own fail on it: the browser, and `kubectl oidc-login` of a kubeconfig
# downloaded from the portal, which has to reach the OIDC issuer.
#
# trust reads the CA from the cluster and adds it to the trust store: the
# login keychain on macOS (for TLS servers only), the system store on Linux
# (needs sudo; browsers with a store of their own are not covered).
# untrust removes every CA of that name, including the ones of earlier
# clusters: a new kind cluster has a new CA, so run trust again after
# recreating it.
#
# The key of the CA is in the kind cluster. Trust it on development machines
# only.
#
# KIND_CLUSTER_NAME selects the cluster (default platform-mesh) and
# PLATFORM_NAMESPACE the namespace of the domain-certificate secret (default
# platform-mesh-system).

die() { echo "$@" >&2; exit 1; }

# The CA cert-manager issues for the gateway, charts/infra.
ca_name="platform-mesh-gateway-ca"
linux_name="platform-mesh-tilt-ca"

action="$1"
case "$action" in
    trust | untrust) ;;
    *) die "usage: $0 trust|untrust" ;;
esac

os="$(uname -s)"
keychain="$HOME/Library/Keychains/login.keychain-db"

# Debian and Fedora families keep extra CAs in different places.
linux_store() {
    if command -v update-ca-certificates > /dev/null 2>&1; then
        echo "/usr/local/share/ca-certificates/$linux_name.crt"
    elif command -v update-ca-trust > /dev/null 2>&1; then
        echo "/etc/pki/ca-trust/source/anchors/$linux_name.crt"
    else
        die "neither update-ca-certificates nor update-ca-trust found; add the CA to the trust store by hand"
    fi
}

linux_refresh() {
    if command -v update-ca-certificates > /dev/null 2>&1; then
        sudo update-ca-certificates
    else
        sudo update-ca-trust
    fi
}

untrust() {
    case "$os" in
        Darwin)
            # One SHA-1 per certificate of that name, also of earlier clusters.
            hashes="$(security find-certificate -a -c "$ca_name" -Z "$keychain" 2> /dev/null \
                | sed -n 's/^SHA-1 hash: //p')"
            for hash in $hashes; do
                security delete-certificate -Z "$hash" -t "$keychain" \
                    || die "removing $ca_name ($hash) from the login keychain failed"
                echo "removed $ca_name ($hash) from the login keychain"
            done
            ;;
        Linux)
            store="$(linux_store)" || exit 1
            if test -e "$store"; then
                sudo rm -f "$store" || die "removing $store failed"
                linux_refresh || die "refreshing the trust store failed"
                echo "removed $store"
            fi
            ;;
        *) die "unsupported OS $os; remove the CA $ca_name from the trust store by hand" ;;
    esac
}

trust() {
    repo="$(git rev-parse --show-toplevel)" || die "not in the repository"
    ca="$repo/contrib/tilt/.secret/ca.crt"

    # From the cluster, not the copy Tilt exported: that one is stale after
    # the cluster was recreated.
    KUBE_CONTEXT="kind-${KIND_CLUSTER_NAME:-platform-mesh}" NAMESPACE="${PLATFORM_NAMESPACE:-platform-mesh-system}" \
        "$repo/contrib/tilt/hack/export-secret.sh" domain-certificate ca.crt "$ca" \
        || die "reading the CA failed; is the Tilt environment up (task tilt:up)?"

    subject="$(openssl x509 -in "$ca" -noout -subject)" || die "$ca is not a certificate"
    case "$subject" in
        *"$ca_name"*) ;;
        *) die "$ca is not the CA $ca_name: $subject" ;;
    esac

    # CAs of earlier clusters have the same name and are of no use anymore.
    untrust || exit 1

    case "$os" in
        Darwin)
            # Asks for the login password. -p ssl: for TLS servers only.
            security add-trusted-cert -r trustRoot -p ssl -k "$keychain" "$ca" \
                || die "adding $ca to the login keychain failed"
            echo "trusted $ca_name in the login keychain; remove it with: task tilt:untrust"
            ;;
        Linux)
            store="$(linux_store)" || exit 1
            sudo install -m 0644 "$ca" "$store" || die "writing $store failed"
            linux_refresh || die "refreshing the trust store failed"
            echo "trusted $ca_name as $store; remove it with: task tilt:untrust"
            ;;
        *) die "unsupported OS $os; add $ca to the trust store by hand" ;;
    esac
}

"$action"
