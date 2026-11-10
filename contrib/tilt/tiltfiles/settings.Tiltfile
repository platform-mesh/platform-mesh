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

# Tilt settings and the constants every sub-Tiltfile shares.
# Evaluated once, load() the names needed.

load("../helpers.py", "REPO")

DEFAULT_CONTEXT = "kind-platform-mesh"
DEFAULT_HTTPBIN_NAMESPACE = "example-httpbin-provider"

config.define_string("context", usage="kube context to deploy into (default {}, see hack/kind.sh)".format(DEFAULT_CONTEXT))
config.define_string(
    "httpbin-namespace", usage="HTTPBin provider runtime namespace (default {})".format(DEFAULT_HTTPBIN_NAMESPACE)
)
config.define_string("cluster-id", usage="dashed node IP used in hostnames (default: InternalIP of the first node)")
config.define_bool(
    "frontend-cors",
    usage="allow browser calls from a frontend run outside the cluster (see installation/frontend-dev-cors-values.yaml)",
)

# Backends the environment can run without, and the PlatformMesh feature
# toggle that makes platform-mesh-operator skip their kcp manifests. The
# sub-Tiltfiles skip the backend and everything that only exists for it.
WITHOUT_TOGGLES = {
    "keycloak": "feature-disable-keycloak",
}
config.define_string_list(
    "without",
    usage="comma-separated backends to leave out: {} (repeatable)".format("|".join(WITHOUT_TOGGLES.keys())),
)
cfg = config.parse()


def parse_without(args):
    """Backends named by --without, deduplicated and validated.

    string_list accumulates repeated flags but does not split on commas.
    """
    names = []
    for arg in args:
        for name in arg.split(","):
            name = name.strip()
            if not name:
                continue
            if name not in WITHOUT_TOGGLES:
                fail("--without={}: expected one or more of {}".format(name, ", ".join(WITHOUT_TOGGLES.keys())))
            if name not in names:
                names.append(name)
    return names


CONTEXT = cfg.get("context", DEFAULT_CONTEXT)
NAMESPACE = "platform-mesh-system"
HTTPBIN_NAMESPACE = cfg.get("httpbin-namespace", DEFAULT_HTTPBIN_NAMESPACE)
FRONTEND_CORS = cfg.get("frontend-cors", False)
WITHOUT = parse_without(cfg.get("without", []))
KEYCLOAK = "keycloak" not in WITHOUT
# Toggles for the PlatformMesh CR (manifests/platformmesh.yaml).
FEATURE_TOGGLES = ["feature-accounts-in-accounts"] + [WITHOUT_TOGGLES[name] for name in WITHOUT]
KUBECTL = "kubectl --context {} -n {}".format(CONTEXT, NAMESPACE)
GO_DEPS = ["apis", "subroutines", "golang-commons"]


def third_party_version(name, resource):
    """Version of a resource of the wrapper third_party/<name>/component-constructor.yaml."""
    constructor = read_yaml(os.path.join(REPO, "third_party", name, "component-constructor.yaml"))
    for r in constructor["components"][0]["resources"]:
        if r["name"] == resource:
            return r["version"]
    fail("third_party/{}: no resource {}".format(name, resource))


# Every hostname is <x>.pm.<node-ip-dashed>.sslip.io, reaching the kind
# node's traefik NodePort from host, pods and kcp alike.
# hack/kind.sh makes CoreDNS answer sslip.io in-cluster.
CLUSTER_ID = cfg.get("cluster-id", "") or str(
    local(
        "kubectl --context {} get nodes -o jsonpath='{{.items[0].status.addresses[?(@.type==\"InternalIP\")].address}}'".format(
            CONTEXT
        ),
        quiet=True,
        echo_off=True,
    )
).strip().replace(".", "-")
if not CLUSTER_ID:
    fail("could not determine the node InternalIP; run hack/kind.sh up or pass --cluster-id")
BASE_DOMAIN = "pm.{}.sslip.io".format(CLUSTER_ID)
PORT = 31000
BASE_DOMAIN_PORT = "{}:{}".format(BASE_DOMAIN, PORT)
KCP_URL = "https://kcp.api." + BASE_DOMAIN_PORT

# In-cluster front proxy URL. kcp-operator gives the Service the FrontProxy's
# external port, so it is the NodePort here (the kind install has 8443 twice).
KCP_FRONT_PROXY_URL = "https://frontproxy-front-proxy.{}:{}".format(NAMESPACE, PORT)

# Values the kind-profile templates with (pkg/subroutines/subroutine_helpers.go
# getExposureParams), plus the __PLACEHOLDER__ tokens the PMO chart replaces.
# gatewayClusterIP only feeds hostAliases, which are dropped.
TEMPLATE_VARS = {
    "baseDomain": BASE_DOMAIN,
    "baseDomainPort": BASE_DOMAIN_PORT,
    "port": str(PORT),
    "gatewayClusterIP": "",
    "internalFrontProxyUrl": KCP_FRONT_PROXY_URL,
    "gatewayClassName": "traefik",
}
