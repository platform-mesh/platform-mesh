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

# Prints an OCM CLI configuration (.ocmconfig) to stdout with
#   - credentials for the OCI registry the components are published to, and
#   - the RSA signing key used for `ocm sign component-version`.
#
# Usage:
#   ocm-config.sh <ocm-repository> [<signature-name>] > ~/.ocmconfig
#
#   <ocm-repository>   e.g. ghcr.io/platform-mesh
#   <signature-name>   OCM signature name; must equal the CN of the signing
#                      certificate. If omitted, no signing section is written.
#
# Environment:
#   GITHUB_TOKEN              registry password (username "github"); if unset
#                             or empty, no credentials section is written and
#                             the registry is accessed anonymously
#   REGISTRY_USERNAME         override the registry username
#   OCM_SIGNING_PRIVATE_KEY   PEM private key   (required with <signature-name>)
#   OCM_SIGNING_CERT          PEM certificate   (required with <signature-name>)

set -euo pipefail

repo="${1:?usage: $0 <ocm-repository> [<signature-name>]}"
signature="${2:-}"

# Credentials are configured per registry host, not per path: OCM v2 matches
# a consumer's path attribute with path.Match (a glob), so "platform-mesh"
# would not match a request for "platform-mesh/platform-mesh/<chart>".
hostname="${repo%%/*}"

indent() { sed "s/^/$1/"; }

echo "type: generic.config.ocm.software/v1"
echo "configurations:"

if [ -n "${GITHUB_TOKEN:-}" ]; then
  cat <<EOF
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: OCIRegistry
          scheme: https
          hostname: ${hostname}
        credentials:
          - type: Credentials
            properties:
              username: ${REGISTRY_USERNAME:-github}
              password: ${GITHUB_TOKEN}
EOF
fi

if [ -n "$signature" ]; then
  : "${OCM_SIGNING_PRIVATE_KEY:?OCM_SIGNING_PRIVATE_KEY must be set to sign}"
  : "${OCM_SIGNING_CERT:?OCM_SIGNING_CERT must be set to sign}"
  cat <<EOF
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: RSA/v1alpha1
          algorithm: RSASSA-PSS
          signature: ${signature}
        credentials:
          - type: Credentials/v1
            properties:
              private_key_pem: |
$(printf '%s\n' "$OCM_SIGNING_PRIVATE_KEY" | indent '                ')
              public_key_pem: |
$(printf '%s\n' "$OCM_SIGNING_CERT" | indent '                ')
EOF
fi
