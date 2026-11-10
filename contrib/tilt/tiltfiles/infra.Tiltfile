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

# Infra: external dependencies managed here. kcp-operator, openfga
# (charts the kind-profile marks external) and the infra chart (kcp
# RootShard/FrontProxy, postgres, keycloak, gateway).

load("../helpers.py", "TILT_DIR", "kind_in", "profile_chart", "profile_values_file", "remote_chart", "select_objects", "write_yaml")
load(
    "./settings.Tiltfile",
    "BASE_DOMAIN",
    "KEYCLOAK",
    "KUBECTL",
    "OPENFGA",
    "NAMESPACE",
    "PORT",
    "TEMPLATE_VARS",
    "third_party_version",
)

kcp_operator = remote_chart(
    "kcp-operator",
    "https://kcp-dev.github.io/helm-charts",
    third_party_version("kcp-operator", "chart"),
    NAMESPACE,
)
k8s_yaml(kcp_operator)
k8s_resource(
    "kcp-operator",
    objects=select_objects(kcp_operator, lambda o: True),
    resource_deps=["cert-manager-startupapicheck"],
    labels=["infra"],
)

if OPENFGA:
    openfga = remote_chart(
        "openfga",
        "oci://ghcr.io/openfga/helm-charts",
        third_party_version("openfga", "chart"),
        NAMESPACE,
        values=[profile_values_file("openfga", TEMPLATE_VARS)],
    )
    k8s_yaml(openfga)
    # openfga now uses the shared CNPG postgres (see the openfga-cnpg migration);
    # it no longer ships its own postgres, so wait on the "postgres-ready" resource.
    k8s_resource(
        "openfga",
        objects=select_objects(openfga, lambda o: True),
        resource_deps=["namespace", "postgres-ready"],
        labels=["infra"],
    )

# The profile's 8443 literals mean "external port"; here that is the NodePort.

# Resources for pods created by operators from CRs, where _strip_resources cannot reach.
# Requests near zero rather than empty, since kcp-operator defaults
# empty shard resources to 1 CPU / 1 GiB and the infra chart's etcd
# helper `default`s empty values. Limits stay where the chart sets them;
# Kubernetes would otherwise copy them into the requests.
NO_REQUESTS = {"requests": {"cpu": "1m", "memory": "1Mi"}}

KCP_ROOT_HOSTNAME = "root.kcp." + BASE_DOMAIN
KCP_TRITON_HOSTNAME = "triton.kcp." + BASE_DOMAIN

infra = profile_chart(
    "infra",
    NAMESPACE,
    TEMPLATE_VARS,
    overrides={
        "kcp": {
            "external": {"port": PORT},
            "rootShard": {"shardBaseURL": "https://{}:{}/".format(KCP_ROOT_HOSTNAME, PORT), "resources": NO_REQUESTS},
            "shards": [{"name": "triton", "hostname": KCP_TRITON_HOSTNAME, "spec": {"resources": NO_REQUESTS}}],
            # kcp-operator defaults both to 2; one is enough on a single node.
            "proxy": {"replicas": 1},
            "cacheServer": {"replicas": 1, "resources": NO_REQUESTS},
            "frontProxy": {"resources": NO_REQUESTS},
            "etcd": {"resources": NO_REQUESTS, "backup": {"resources": NO_REQUESTS}},
            # The ReBAC authorization webhook is rebac-authz-webhook, which needs OpenFGA.
            "webhook": {"enabled": OPENFGA},
        },
        "keycloak": {"operator": {"enabled": KEYCLOAK, "resources": NO_REQUESTS}},
    },
)
k8s_yaml(infra)

# Wait on operator conditions because bootstrap replaces kcp pods.
k8s_resource(
    new_name="kcp",
    objects=select_objects(
        infra, kind_in(["FrontProxy", "CacheServer", "Etcd", "Kubeconfig", "Issuer", "Certificate"])
    ),
    resource_deps=["kcp-operator", "etcd-druid", "domain-certificate-ready"],
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

# wait for kcp components to report being healthy and ready before proceeding
# this handles two cases:
# 1. kcp needing time to setup and sync
# 2. ingress manager needing time to program the gateway
KCP_GATEWAY_HOSTNAMES = ["kcp.api." + BASE_DOMAIN, KCP_ROOT_HOSTNAME, KCP_TRITON_HOSTNAME]
KCP_GATEWAY_ATTEMPTS = 60
KCP_GATEWAY_RETRY_SECONDS = 5
local_resource(
    "kcp-gateway-ready",
    labels=["infra"],
    resource_deps=["kcp-ready", "gateway", "export-kcp-ca"],
    allow_parallel=True,
    dir=TILT_DIR,
    cmd=(
        "for url in {urls}; do curl -fsS --cacert .secret/kcp-root-ca.crt --retry {attempts} --retry-delay {retry} --retry-all-errors \"$url\" || exit 1; done"
    ).format(
        urls=" ".join(["https://{}:{}/readyz".format(h, PORT) for h in KCP_GATEWAY_HOSTNAMES]),
        attempts=KCP_GATEWAY_ATTEMPTS,
        retry=KCP_GATEWAY_RETRY_SECONDS,
    ),
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

# "postgres" turns Ready as soon as any cnpg.io/cluster pod is Ready, which
# includes the short-lived initdb pod while initdb is still running. Consumers
# (openfga's migrate-database init container, keycloak) then hit a database
# that does not exist yet, and tilt ci aborts on the first Init:Error. Gate
# them on the Cluster being Ready and every Database having been applied.
local_resource(
    "postgres-ready",
    labels=["infra"],
    resource_deps=["postgres"],
    allow_parallel=True,
    cmd=KUBECTL
    + " wait clusters.postgresql.cnpg.io/platform-mesh-pg --for=condition=Ready --timeout=5m && "
    + KUBECTL
    + " wait databases.postgresql.cnpg.io --all --for=jsonpath='{.status.applied}'=true --timeout=5m",
)

# Without Keycloak the chart still renders its HTTPRoute (it serves external
# Keycloaks too); /keycloak then answers 503.
if KEYCLOAK:
    k8s_resource(
        new_name="keycloak",
        objects=select_objects(
            infra,
            lambda o: o["kind"] == "Keycloak" or (o["kind"] == "Secret" and o["metadata"]["name"].startswith("keycloak-")),
        ),
        resource_deps=["keycloak-operator", "postgres-ready", "domain-certificate-ready"],
        labels=["infra"],
        extra_pod_selectors=[{"app": "keycloak"}],
        pod_readiness="wait",
    )

k8s_resource(
    new_name="gateway",
    objects=select_objects(infra, kind_in(["Gateway", "HTTPRoute", "TLSRoute"])),
    resource_deps=["gatewayclass", "domain-certificate-ready"],
    labels=["infra"],
)
