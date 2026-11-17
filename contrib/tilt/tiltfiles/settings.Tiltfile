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
config.define_string("cluster-id", usage="dashed IP in the hostnames (default 127-0-0-1, the host loopback)")
config.define_bool(
    "frontend-cors",
    usage="allow browser calls from a frontend run outside the cluster (see installation/frontend-dev-cors-values.yaml)",
)

# Backends the environment can run without, and the PlatformMesh feature
# toggle that makes platform-mesh-operator skip their kcp manifests. The
# sub-Tiltfiles skip the backend and everything that only exists for it.
WITHOUT_TOGGLES = {
    "keycloak": "feature-disable-keycloak",
    "openfga": "feature-disable-openfga",
}
config.define_string_list(
    "without",
    usage="comma-separated backends to leave out: {} (repeatable)".format("|".join(WITHOUT_TOGGLES.keys())),
)

WITH_BACKENDS = ["dex"]
config.define_string_list(
    "with",
    usage="comma-separated backends to add: {} (repeatable); dex leaves Keycloak out".format("|".join(WITH_BACKENDS)),
)
cfg = config.parse()


def parse_backends(flag, args, known):
    """Backends named by a list flag, deduplicated and validated.

    string_list accumulates repeated flags but does not split on commas.
    """
    names = []
    for arg in args:
        for name in arg.split(","):
            name = name.strip()
            if not name:
                continue
            if name not in known:
                fail("--{}={}: expected one or more of {}".format(flag, name, ", ".join(known)))
            if name not in names:
                names.append(name)
    return names


CONTEXT = cfg.get("context", DEFAULT_CONTEXT)
NAMESPACE = "platform-mesh-system"
HTTPBIN_NAMESPACE = cfg.get("httpbin-namespace", DEFAULT_HTTPBIN_NAMESPACE)
FRONTEND_CORS = cfg.get("frontend-cors", False)
WITH = parse_backends("with", cfg.get("with", []), WITH_BACKENDS)
WITHOUT = parse_backends("without", cfg.get("without", []), WITHOUT_TOGGLES.keys())
DEX = "dex" in WITH
# Dex replaces Keycloak as identity provider.
if DEX and "keycloak" not in WITHOUT:
    WITHOUT.append("keycloak")
KEYCLOAK = "keycloak" not in WITHOUT
OPENFGA = "openfga" not in WITHOUT
# Toggles for the PlatformMesh CR (manifests/platformmesh.yaml).
FEATURE_TOGGLES = ["feature-accounts-in-accounts"] + [WITHOUT_TOGGLES[name] for name in WITHOUT]
if KEYCLOAK:
    # No SMTP on kind: nothing verifies the e-mail of a user who signs up in
    # the welcome realm. Org realms get the same from pm-keycloak-operator's
    # idp.allowUnverifiedEmails (platform-mesh.Tiltfile).
    FEATURE_TOGGLES.append("feature-disable-email-verification")
KUBECTL = "kubectl --context {} -n {}".format(CONTEXT, NAMESPACE)
GO_DEPS = ["apis", "subroutines", "golang-commons"]


def third_party_version(name, resource):
    """Version of a resource of the wrapper third_party/<name>/component-constructor.yaml."""
    constructor = read_yaml(os.path.join(REPO, "third_party", name, "component-constructor.yaml"))
    for r in constructor["components"][0]["resources"]:
        if r["name"] == resource:
            return r["version"]
    fail("third_party/{}: no resource {}".format(name, resource))


# Hostnames are <x>.pm.127-0-0-1.sslip.io: sslip.io answers the host loopback,
# where contrib/tilt/kind.yaml publishes the gateway NodePort. hack/kind.sh
# answers the node address for the same names, in-cluster.
CLUSTER_ID = cfg.get("cluster-id", "127-0-0-1")
BASE_DOMAIN = "pm.{}.sslip.io".format(CLUSTER_ID)
PORT = 31000 # the same port as mapped by the kind config
BASE_DOMAIN_PORT = "{}:{}".format(BASE_DOMAIN, PORT)
KCP_URL = "https://kcp.api." + BASE_DOMAIN_PORT

# In-cluster front proxy URL. kcp-operator gives the Service the FrontProxy's
# external port, so it is the NodePort here (the kind install has 8443 twice).
KCP_FRONT_PROXY_URL = "https://frontproxy-front-proxy.{}:{}".format(NAMESPACE, PORT)

# Values the kind-profile templates with (pkg/subroutines/subroutine_helpers.go
# getExposureParams). gatewayServiceClusterIP only feeds hostAliases, which
# are dropped.
TEMPLATE_VARS = {
    "baseDomain": BASE_DOMAIN,
    "baseDomainPort": BASE_DOMAIN_PORT,
    "port": str(PORT),
    "gatewayServiceClusterIP": "",
    "internalFrontProxyUrl": KCP_FRONT_PROXY_URL,
}

# Dex as identity provider of kcp (--with=dex): static users.
DEX_ISSUER = "https://{}/dex".format(BASE_DOMAIN_PORT)
DEX_CLIENT_ID = "platform-mesh"
# Local development only.
DEX_CLIENT_SECRET = "local-dev-platform-mesh-secret"
# Dex adds e-mail and groups to a token only for their scopes, offline_access gives a refresh token.
DEX_SCOPE = "openid email profile groups offline_access"
# bcrypt of "password".
DEX_PASSWORD_HASH = "$2a$10$iwYOwbUyunwL.Ahtr9zBq.ruc.rjYSDPwBnX.CDAsMlUA1alMoeqW"
DEX_USERS = [
    {
        "email": "admin@portal.localhost",
        "username": "admin",
        "userID": "69b09731-2f2e-454e-b08c-2186ec99c75e",
        "hash": DEX_PASSWORD_HASH,
        "groups": ["platform-mesh:admins"],
    },
    {
        "email": "dev@portal.localhost",
        "username": "dev",
        "userID": "186d270a-8446-4c96-bbb8-6aa95ffdb6e7",
        "hash": DEX_PASSWORD_HASH,
        "groups": ["platform-mesh:developers"],
    },
    {
        "email": "viewer@portal.localhost",
        "username": "viewer",
        "userID": "9858cb7f-f74f-4932-818f-56d69459dac1",
        "hash": DEX_PASSWORD_HASH,
        "groups": ["platform-mesh:viewers"],
    },
]
