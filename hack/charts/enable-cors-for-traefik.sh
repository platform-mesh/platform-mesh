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

set -euo pipefail

# Traefik doesn't support CORS configuration on HTTPRoute filters,
# so we need to create a Middleware and reference it in the HTTPRoute filters
# instead of the CORS filter.

NAMESPACE="platform-mesh-system"

# Create the Traefik CORS Middleware
kubectl apply -f - <<EOF
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: cors-header
  namespace: ${NAMESPACE}
spec:
  headers:
    accessControlAllowMethods:
      - "GET"
      - "OPTIONS"
      - "POST"
    accessControlAllowHeaders:
      - "*"
    accessControlAllowOriginList:
      - "https://portal.localhost"
      - "https://*.portal.localhost"
    accessControlAllowCredentials: true
    accessControlMaxAge: 100
    addVaryHeader: true
EOF

# Patch iam-service HTTPRoute: replace CORS filter with ExtensionRef, keep URLRewrite
kubectl patch httproute iam-service -n "${NAMESPACE}" --type=json -p='[
  {
    "op": "replace",
    "path": "/spec/rules/0/filters",
    "value": [
      {
        "type": "ExtensionRef",
        "extensionRef": {
          "group": "traefik.io",
          "kind": "Middleware",
          "name": "cors-header"
        }
      },
      {
        "type": "URLRewrite",
        "urlRewrite": {
          "path": {
            "type": "ReplacePrefixMatch",
            "replacePrefixMatch": "/graphql"
          }
        }
      }
    ]
  }
]'

# Patch kubernetes-graphql-gateway HTTPRoute: replace CORS filter with ExtensionRef, keep URLRewrite
kubectl patch httproute kubernetes-graphql-gateway -n "${NAMESPACE}" --type=json -p='[
  {
    "op": "replace",
    "path": "/spec/rules/0/filters",
    "value": [
      {
        "type": "ExtensionRef",
        "extensionRef": {
          "group": "traefik.io",
          "kind": "Middleware",
          "name": "cors-header"
        }
      },
      {
        "type": "URLRewrite",
        "urlRewrite": {
          "path": {
            "type": "ReplacePrefixMatch",
            "replacePrefixMatch": "/api/clusters/"
          }
        }
      }
    ]
  }
]'

# Patch keycloak HTTPRoute: replace CORS filter with ExtensionRef
kubectl patch httproute keycloak -n "${NAMESPACE}" --type=json -p='[
  {
    "op": "replace",
    "path": "/spec/rules/0/filters",
    "value": [
      {
        "type": "ExtensionRef",
        "extensionRef": {
          "group": "traefik.io",
          "kind": "Middleware",
          "name": "cors-header"
        }
      }
    ]
  }
]'

echo "Done. cors-header Middleware created and HTTPRoutes patched."
