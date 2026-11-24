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

# Tilt settings and the constants every component shares.
# Evaluated once, load() the names needed.

load("./helpers.py", "REPO")

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

config.define_string_list(
    "without",
    usage="components to leave out, with everything that depends on them, e.g. keycloak,openfga (comma-separated, repeatable)",
)
config.define_string_list(
    "groups",
    args=True,
    usage="groups or components to run, with everything they need (default: see groups.Tiltfile)",
)

WITH_BACKENDS = ["dex"]
config.define_string_list(
    "with",
    usage="comma-separated components to add to what runs: {} (repeatable); dex leaves Keycloak out".format(
        "|".join(WITH_BACKENDS)
    ),
)
config.define_string(
    "access-vw-src",
    usage="checkout of kcp-dev/contrib-access-virtual-workspace to build kcp-access-vw from, rebuilt on every change (default: the released image)",
)
cfg = config.parse()


def split_list(args):
    """Names from a repeatable, comma-separated flag, deduplicated.

    string_list accumulates repeated flags but does not split on commas.
    """
    names = []
    for arg in args:
        for name in arg.split(","):
            name = name.strip()
            if name and name not in names:
                names.append(name)
    return names


CONTEXT = cfg.get("context", DEFAULT_CONTEXT)
NAMESPACE = "platform-mesh-system"
HTTPBIN_NAMESPACE = cfg.get("httpbin-namespace", DEFAULT_HTTPBIN_NAMESPACE)
FRONTEND_CORS = cfg.get("frontend-cors", False)
# What to run: validated and resolved against the components by the root Tiltfile.
WANTED = split_list(cfg.get("groups", []))
WITHOUT = split_list(cfg.get("without", []))
# --with=dex: Dex is the "dex" component, and replaces Keycloak as identity provider.
WITH = split_list(cfg.get("with", []))
for name in WITH:
    if name not in WITH_BACKENDS:
        fail("--with={}: expected one or more of {}".format(name, ", ".join(WITH_BACKENDS)))
if "dex" in WITH and "keycloak" not in WITHOUT:
    WITHOUT.append("keycloak")
KUBECTL = "kubectl --context {} -n {}".format(CONTEXT, NAMESPACE)


def _checkout(path):
    """Absolute path of a source checkout given on the command line, "" if none."""
    if not path:
        return ""
    if path.startswith("~/"):
        path = os.path.join(os.getenv("HOME"), path[2:])
    path = os.path.abspath(path)
    if not os.path.exists(os.path.join(path, "go.mod")):
        fail("{}: no go.mod, expected a source checkout".format(path))
    return path


# --access-vw-src: build the access virtual workspace from this checkout.
ACCESS_VW_SRC = _checkout(cfg.get("access-vw-src", ""))
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

# Runtime inputs for the shared chart contract and the remaining static-asset
# templates. Tilt disables the kind profile's retained host aliases.
TEMPLATE_VARS = {
    "baseDomain": BASE_DOMAIN,
    "baseDomainPort": BASE_DOMAIN_PORT,
    "port": str(PORT),
    "internalFrontProxyUrl": KCP_FRONT_PROXY_URL,
    "gatewayClassName": "traefik",
}

# Dex as identity provider of kcp (--with=dex): static users.
DEX_ISSUER = "https://{}/dex".format(BASE_DOMAIN_PORT)
DEX_CLIENT_ID = "platform-mesh"
# Local development only.
DEX_CLIENT_SECRET = "local-dev-platform-mesh-secret"
# Dex adds e-mail and groups to a token only for their scopes, offline_access gives a refresh token.
DEX_SCOPES = ["openid", "email", "profile", "groups", "offline_access"]
# What kubeconfigs from the portal request besides openid: the username claim of kcp, and the groups.
DEX_KUBECTL_SCOPES = ["email", "groups"]
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
