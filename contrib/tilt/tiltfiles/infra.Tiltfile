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

# Infra: external dependencies managed here. kcp-operator, openfga, init-agent
# (charts the kind-profile marks external) and the infra chart (kcp
# RootShard/FrontProxy, postgres, keycloak, gateway).

load("../helpers.py", "kind_in", "profile_chart", "profile_values_file", "remote_chart", "select_objects", "write_yaml")
load(
    "./settings.Tiltfile",
    "BASE_DOMAIN",
    "BASE_DOMAIN_PORT",
    "KUBECTL",
    "NAMESPACE",
    "PORT",
    "TEMPLATE_VARS",
    "VERSIONS",
)

kcp_operator = remote_chart(
    "kcp-operator",
    "oci://ghcr.io/platform-mesh/helm-charts/charts/mirrored",
    VERSIONS["KCP_OPERATOR_CHART_VERSION"],
    NAMESPACE,
)
k8s_yaml(kcp_operator)
k8s_resource(
    "kcp-operator",
    objects=select_objects(kcp_operator, lambda o: True),
    resource_deps=["cert-manager-startupapicheck"],
    labels=["infra"],
)

openfga = remote_chart(
    "openfga",
    "oci://ghcr.io/platform-mesh/ocm/charts",
    VERSIONS["OPENFGA_VERSION"],
    NAMESPACE,
    values=[profile_values_file("openfga", TEMPLATE_VARS)],
)
k8s_yaml(openfga)
k8s_resource(
    "openfga",
    objects=select_objects(openfga, lambda o: not o["metadata"]["name"].startswith("openfga-postgres")),
    resource_deps=["namespace", "openfga-postgres"],
    labels=["infra"],
)
k8s_resource(
    "openfga-postgres",
    objects=select_objects(openfga, lambda o: o["metadata"]["name"].startswith("openfga-postgres")),
    resource_deps=["namespace"],
    labels=["infra"],
)

init_agent = remote_chart(
    "init-agent",
    "oci://ghcr.io/platform-mesh/ocm/charts",
    VERSIONS["INIT_AGENT_CHART_VERSION"],
    NAMESPACE,
    values=[profile_values_file("init-agent", TEMPLATE_VARS)],
)
k8s_yaml(init_agent)
k8s_resource(
    "init-agent",
    objects=select_objects(init_agent, lambda o: True),
    resource_deps=["platform-mesh-operator"],
    labels=["infra"],
)

# The profile's 8443 literals mean "external port"; here that is the NodePort.
infra = profile_chart(
    "infra",
    NAMESPACE,
    TEMPLATE_VARS,
    overrides={
        "kcp": {
            "external": {"port": PORT},
            "rootShard": {"shardBaseURL": "https://root.kcp.{}/".format(BASE_DOMAIN_PORT)},
            "shards": [{"name": "triton", "hostname": "triton.kcp." + BASE_DOMAIN}],
            # kcp-operator defaults both to 2; one is enough on a single node.
            "proxy": {"replicas": 1},
            "cacheServer": {"replicas": 1},
        },
    },
)
k8s_yaml(infra)

# Wait on operator conditions because bootstrap replaces kcp pods.
k8s_resource(
    new_name="kcp",
    objects=select_objects(
        infra, kind_in(["FrontProxy", "CacheServer", "Etcd", "Kubeconfig", "Issuer", "Certificate"])
    ),
    resource_deps=["kcp-operator", "etcd-druid", "domain-certificate"],
    labels=["infra"],
)
k8s_resource(
    new_name="kcp-root", objects=["root:rootshard"], resource_deps=["kcp"], labels=["infra"], pod_readiness="ignore"
)
local_resource(
    "kcp-ready",
    labels=["infra"],
    resource_deps=["kcp-root"],
    allow_parallel=True,
    cmd=KUBECTL + " wait rootshard/root frontproxy/frontproxy --for=condition=Available --timeout=15m",
)
# Second shard, exercised by the e2e tests only.
k8s_resource(
    new_name="kcp-triton", objects=["triton:shard"], resource_deps=["kcp"], labels=["e2e"], pod_readiness="ignore"
)
local_resource(
    "kcp-triton-ready",
    labels=["e2e"],
    resource_deps=["kcp-triton", "kcp-ready"],
    allow_parallel=True,
    cmd=KUBECTL
    + " wait shards.operator.kcp.io/triton --for=create --timeout=5m && "
    + KUBECTL
    + " wait shards.operator.kcp.io/triton --for=condition=Available --timeout=15m",
)

# The cnpg Deployment is Ready before its webhook server listens; a server-side
# dry run of the chart's own Database is the one probe that goes through it.
cnpg_probe = write_yaml(".cache/probe-cnpg.yaml", [o for o in decode_yaml_stream(infra) if o["kind"] == "Database"][0])
local_resource(
    "cnpg-ready",
    labels=["infra"],
    resource_deps=["cnpg"],
    allow_parallel=True,
    cmd="for i in $(seq 60); do {k} apply --dry-run=server -f {f} && exit 0; sleep 2; done; exit 1".format(
        k=KUBECTL, f=cnpg_probe
    ),
)
k8s_resource(
    new_name="postgres",
    objects=select_objects(
        infra, lambda o: kind_in(["Cluster", "Database"])(o) or o["metadata"]["name"].startswith("cnpg-")
    ),
    resource_deps=["cnpg-ready"],
    labels=["infra"],
    extra_pod_selectors=[{"cnpg.io/cluster": "platform-mesh-pg"}],
    pod_readiness="wait",
)
k8s_resource(
    new_name="keycloak",
    objects=select_objects(
        infra,
        lambda o: o["kind"] == "Keycloak" or (o["kind"] == "Secret" and o["metadata"]["name"].startswith("keycloak-")),
    ),
    resource_deps=["keycloak-operator", "postgres", "domain-certificate"],
    labels=["infra"],
    extra_pod_selectors=[{"app": "keycloak"}],
    pod_readiness="wait",
)
k8s_resource(
    new_name="gateway",
    objects=select_objects(infra, kind_in(["Gateway", "HTTPRoute", "TLSRoute"])),
    resource_deps=["traefik-gateway", "domain-certificate"],
    labels=["infra"],
)
