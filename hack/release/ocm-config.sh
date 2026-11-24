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
#   - the oidc information used for `ocm sign component-version`.
#
# Usage:
#   ocm-config.sh <ocm-repository> [<signature-name>] > ~/.ocmconfig
#
#   <ocm-repository>   e.g. ghcr.io/platform-mesh
#   <signature-name>   OCM signature name the key signs and verifies under.
#                      If omitted, no signing section is written.
#
# Environment:
#   GITHUB_TOKEN              registry password (username "github"); if unset
#                             or empty, no credentials section is written and
#                             the registry is accessed anonymously
#   REGISTRY_USERNAME         override the registry username
#   OCM_SIGNING_IDENTITY      identity to sign as    (required with <signature-name>)
#   OCM_SIGNING_ISSUER        issuer of the identity (required with <signature-name>)
#
# To sign as a human user use these values:
#
#   OCM_SIGNING_IDENTITY=jane.doe@example.org         # your github email
#   OCM_SIGNING_ISSUER=https://github.com/login/oauth
#
# Signing will open a login with sigstore, which will then validate your
# identity and produce the signature for the OCM CV.

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
  - type: credentials.config.ocm.software/v1
    consumers:
      - identities:
          - type: OCIRegistry
            scheme: https
            hostname: ${hostname}
        credentials:
          - type: Credentials/v1
            properties:
              username: ${REGISTRY_USERNAME:-github}
              password: ${GITHUB_TOKEN}
EOF
fi

if [ -n "$signature" ]; then
  : "${OCM_SIGNING_IDENTITY:?OCM_SIGNING_IDENTITY must be set to sign}"
  : "${OCM_SIGNING_ISSUER:?OCM_SIGNING_ISSUER must be set to sign}"
  cat <<EOF
  - type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signature: $signature
      signer:
        type: SigstoreSigningConfiguration/v1alpha1
      verifier:
        type: SigstoreVerificationConfiguration/v1alpha1
        certificateIdentity: $OCM_SIGNING_IDENTITY
        certificateOIDCIssuer: $OCM_SIGNING_ISSUER
EOF
fi
