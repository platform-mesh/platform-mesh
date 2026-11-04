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

# Groups: named sets of components for `task tilt:up -- <group>...`.
#
# A group lists what it adds; everything those components need is deployed
# with them (needs/alongside of each component), and a group may name another
# group. Component names work wherever a group name does.

GROUPS = {
    # kcp behind the gateway, and the credentials to reach it from the host.
    "kcp": ["kcp-gateway", "export-ca", "export-kcp-ca", "export-kcp-admin-kubeconfig"],
    # The smallest Platform Mesh: operator, PlatformMesh and accounts.
    "core": ["kcp", "platformmesh", "account-operator"],
    # Relationship-based authorization on OpenFGA.
    "authz": ["core", "pm-openfga-operator", "pm-openfga-authz-webhook"],
    # Keycloak identity provider, users and roles.
    "iam": ["authz", "pm-keycloak-operator", "iam-service", "iam-ui"],
    # GraphQL gateway and virtual workspaces.
    "api": ["core", "kubernetes-graphql-gateway", "marketplace", "extension-manager-virtual-workspace"],
    # Federation with external actors (e.g. providers, consumers).
    "extern": ["core", "access-operator"],
    # The portal and what it talks to.
    "ui": ["api", "iam", "extern", "extension-manager-operator", "portal", "marketplace-ui"],
    # Optional kcp virtual workspaces behind the front proxy: permission-aware
    # workspace discovery and the MCP server on top of it.
    "vw": ["kcp-access-vw", "kcp-mcp-vw"],
    # Fixtures of the e2e suites: HTTPBin provider and a second kcp shard.
    "e2e": ["httpbin", "kcp-triton"],
    # Everything; what `task tilt:e2e` expects.
    "full": ["ui", "e2e"],
}

# What runs when nothing is named. `tilt ci` (task tilt:ci) brings up
# everything the e2e suites need; `tilt up` leaves their fixtures (the "e2e"
# group) out, they are of no use in the development loop.
DEFAULT_CI = ["full"]
DEFAULT_UP = ["ui"]
