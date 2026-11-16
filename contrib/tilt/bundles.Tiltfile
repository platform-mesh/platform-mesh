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

# Bundles: named sets of components for `task tilt:up -- <bundle>...`.
#
# A bundle lists what it adds; everything those components need is deployed
# with them (needs/alongside of each component), and a bundle may name another
# bundle. Component names work wherever a bundle name does.

BUNDLES = {
    # kcp behind the gateway, and the credentials to reach it from the host.
    "kcp": ["kcp-gateway", "export-ca", "export-kcp-ca", "export-kcp-admin-kubeconfig"],
    # The smallest Platform Mesh: operator, PlatformMesh and accounts.
    "core": ["kcp", "platformmesh", "account-operator"],
    # Relationship-based authorization on OpenFGA.
    "authz": ["core", "pm-openfga-operator", "pm-openfga-authz-webhook"],
    # Keycloak identity provider, users and roles.
    "iam": ["authz", "pm-keycloak-operator", "iam-service", "iam-ui"],
    # GraphQL gateway and virtual workspaces.
    "api": ["core", "kubernetes-graphql-gateway", "virtual-workspaces"],
    # The portal and what it talks to.
    "ui": ["api", "iam", "extension-manager-operator", "portal", "marketplace-ui"],
    # Fixtures of the e2e suites: HTTPBin provider and a second kcp shard.
    "e2e": ["httpbin", "kcp-triton"],
    # Everything; what CI and `task tilt:e2e` expect.
    "full": ["ui", "e2e"],
}

DEFAULT = ["full"]
