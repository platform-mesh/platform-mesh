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

# Writes one key of a Secret to a file, decoded, mode 0600.
#
#   export-secret.sh <secret> <key> <file> [<wait-target>]
#
#   wait-target: resource waited on for condition=Ready before reading
#                the secret, e.g. certificate/platform-mesh-gateway
#
# KUBE_CONTEXT and NAMESPACE select the cluster and namespace.

die() { echo "$@" >&2; exit 1; }

test $# -ge 3 || die "usage: $0 <secret> <key> <file> [<wait-target>]"
test -n "$KUBE_CONTEXT" || die "KUBE_CONTEXT is not set"
test -n "$NAMESPACE" || die "NAMESPACE is not set"

secret="$1"
key="$2"
out="$3"
wait_target="$4"

kubectl() { command kubectl --context "$KUBE_CONTEXT" -n "$NAMESPACE" "$@"; }

if test -n "$wait_target"; then
    kubectl wait "$wait_target" --for=condition=Ready --timeout=5m \
        || die "$wait_target did not become Ready"
fi

# Secrets are read-write only for the user.
umask 077
mkdir -p "$(dirname "$out")" || die "creating $(dirname "$out") failed"

# jsonpath keys with dots are escaped as \.
encoded="$(kubectl get secret "$secret" -o jsonpath="{.data.$(echo "$key" | sed 's/\./\\./g')}")" \
    || die "reading secret $secret failed"
test -n "$encoded" || die "secret $secret has no key $key"

tmp="$(mktemp "$out.XXXXXX")" || die "mktemp failed"
finish() { rm -f "$tmp"; }
trap finish EXIT INT

echo "$encoded" | base64 -d > "$tmp" || die "decoding $secret/$key failed"
mv "$tmp" "$out" || die "writing $out failed"
