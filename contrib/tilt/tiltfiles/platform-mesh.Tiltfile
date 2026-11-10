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

# Platform Mesh: platform-mesh-operator (hot-reloaded, OCM off, empty profile,
# see manifests/platformmesh.yaml), the PlatformMesh CR, and the operators and
# services built from source and live-updated (path given) or from the chart's
# released image. Includes the HTTPBin e2e fixture.

load(
    "../helpers.py",
    "chart",
    "component_binary",
    "render",
    "select_objects",
    "service",
    "writable_rootfs",
    "write_values",
)
load(
    "./settings.Tiltfile",
    "BASE_DOMAIN",
    "FEATURE_TOGGLES",
    "GO_DEPS",
    "HTTPBIN_NAMESPACE",
    "KCP_FRONT_PROXY_URL",
    "KEYCLOAK",
    "KUBECTL",
    "NAMESPACE",
    "PORT",
    "TEMPLATE_VARS",
)

component_binary(
    "platform-mesh-operator",
    "operators/platform-mesh-operator",
    deps=GO_DEPS,
    image="ghcr.io/platform-mesh/platform-mesh/platform-mesh-operator",
    labels=["platform-mesh"],
)

# ocm.enabled=false renders only the operator (no OCM/Flux objects,
# PlatformMesh or profile) and drops the kcp flags and CA mount, which
# are added back here.
pmo = chart(
    "platform-mesh-operator",
    NAMESPACE,
    values=[
        write_values(
            "platform-mesh-operator",
            {
                "ocm": {"enabled": False},
                "log": {"level": "debug"},
                "extraArgs": [
                    "--subroutines-feature-toggles-enabled=true",
                    "--kcp-front-proxy-port={}".format(PORT),
                    "--kcp-url=" + KCP_FRONT_PROXY_URL,
                ],
                "extraEnvs": [{"name": "SSL_CERT_FILE", "value": "/etc/platform-mesh-ca/ca.crt"}],
                "extraVolumeMounts": [
                    {"name": "platform-mesh-ca", "mountPath": "/etc/platform-mesh-ca", "readOnly": True}
                ],
                "extraVolumes": [
                    {
                        "name": "platform-mesh-ca",
                        "secret": {
                            "secretName": "domain-certificate",
                            "optional": True,
                            "items": [{"key": "ca.crt", "path": "ca.crt"}],
                        },
                    }
                ],
            },
        )
    ],
)

pmo = writable_rootfs(pmo, "ghcr.io/platform-mesh/platform-mesh/platform-mesh-operator")
k8s_yaml(pmo)

# Not after 'kcp': the root shard only starts once PMO has written
# kcp-webhook-secret, PMO's kcp setup retries until the front proxy
# answers.
k8s_resource(
    "platform-mesh-operator",
    objects=select_objects(pmo, lambda o: True),
    resource_deps=["kcp-operator", "domain-certificate-ready", "ocm-crds"],
    labels=["platform-mesh"],
)

# Separate resource so re-triggering the operator never delete-recreates
# the CR.
k8s_yaml(
    render(
        "manifests/platformmesh.yaml",
        {
            "BASE_DOMAIN": BASE_DOMAIN,
            "PORT": PORT,
            "HTTPBIN_NAMESPACE": HTTPBIN_NAMESPACE,
            "FEATURE_TOGGLES": "\n".join(["    - name: " + t for t in FEATURE_TOGGLES]),
        },
    )
)
k8s_resource(
    new_name="platformmesh",
    objects=["platform-mesh:platformmesh", "platform-mesh-profile:configmap"],
    resource_deps=["platform-mesh-operator"],
    labels=["platform-mesh"],
)

# Ready means kcp workspaces, APIExports and every *-kubeconfig secret exist.
# Services cannot wait for it, PMO's kcp setup needs their charts' webhook
# Certificates, so they deploy after the operator and start once it has
# written their kubeconfig secrets (as under Flux).
local_resource(
    "platformmesh-ready",
    labels=["platform-mesh"],
    resource_deps=["platformmesh"],
    allow_parallel=True,
    cmd=KUBECTL + " wait platformmesh/platform-mesh --for=condition=Ready --timeout=20m",
)

# --- services
service("account-operator", NAMESPACE, TEMPLATE_VARS, path="operators/account-operator", deps=GO_DEPS)
service(
    "extension-manager-operator",
    NAMESPACE,
    TEMPLATE_VARS,
    path="operators/extension-manager-operator",
    deps=GO_DEPS,
    workloads=["extension-manager-operator-operator", "extension-manager-operator-server"],
)
service(
    "security-operator",
    NAMESPACE,
    TEMPLATE_VARS,
    path="operators/security-operator",
    deps=GO_DEPS,
    workloads=[
        "security-operator",
        "security-operator-generator",
        "security-operator-initializer",
        "security-operator-system",
        "security-operator-terminator",
    ],
)
service(
    "kubernetes-graphql-gateway",
    NAMESPACE,
    TEMPLATE_VARS,
    path="services/kubernetes-graphql-gateway",
    deps=GO_DEPS,
    workloads=["kubernetes-graphql-gateway", "kubernetes-graphql-gateway-listener"],
)
service("rebac-authz-webhook", NAMESPACE, TEMPLATE_VARS, path="services/rebac-authz-webhook", deps=GO_DEPS)
service("virtual-workspaces", NAMESPACE, TEMPLATE_VARS, path="services/virtual-workspaces", deps=GO_DEPS)
service("portal", NAMESPACE, TEMPLATE_VARS, overrides={"frontendPort": str(PORT)})
service("marketplace-ui", NAMESPACE, TEMPLATE_VARS)

# User and role management on top of Keycloak.
if KEYCLOAK:
    service("iam-service", NAMESPACE, TEMPLATE_VARS, path="services/iam-service", deps=GO_DEPS)
    service("iam-ui", NAMESPACE, TEMPLATE_VARS)

include("./httpbin.Tiltfile")
