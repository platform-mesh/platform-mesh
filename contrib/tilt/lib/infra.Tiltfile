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

# The infra chart: kcp (RootShard, FrontProxy, shards), postgres, Keycloak, Dex
# and the gateway. One chart shared by several components: rendered once per run
# with its kind-profile values, each component claims its objects and the
# objects of components that are not selected are dropped.

load("./helpers.py", "CHARTS", "kind_in", "profile_chart", "select_objects")
load(
    "./settings.Tiltfile",
    "BASE_DOMAIN",
    "BASE_DOMAIN_PORT",
    "DEX_CLIENT_ID",
    "DEX_CLIENT_SECRET",
    "DEX_ISSUER",
    "DEX_USERS",
    "GATEWAY_PORT",
    "NAMESPACE",
    "TEMPLATE_VARS",
)

KCP_API_HOSTNAME = "kcp.api." + BASE_DOMAIN
KCP_ROOT_HOSTNAME = "root.kcp." + BASE_DOMAIN
KCP_TRITON_HOSTNAME = "triton.kcp." + BASE_DOMAIN

# Resources for pods created by operators from CRs, where _strip_resources cannot reach.
# Requests near zero rather than empty, since kcp-operator defaults
# empty shard resources to 1 CPU / 1 GiB and the infra chart's etcd
# helper `default`s empty values. Limits stay where the chart sets them;
# Kubernetes would otherwise copy them into the requests.
NO_REQUESTS = {"requests": {"cpu": "1m", "memory": "1Mi"}}

# Component -> predicate for the objects of the chart it owns.
INFRA_OWNED = {
    "kcp": kind_in(["RootShard", "FrontProxy", "CacheServer", "Etcd", "Kubeconfig", "Issuer", "Certificate"]),
    "kcp-triton": kind_in(["Shard"]),
    "postgres": lambda o: kind_in(["Cluster", "Database"])(o) or o["metadata"]["name"].startswith("cnpg-"),
    # Without Keycloak the chart still renders its HTTPRoute (it serves external
    # Keycloaks too); /keycloak then answers 503.
    "keycloak": lambda o: o["kind"] == "Keycloak"
    or (o["kind"] == "Secret" and o["metadata"]["name"].startswith("keycloak-")),
    "gateway": kind_in(["Gateway", "HTTPRoute", "TLSRoute"]),
}


# Component -> (front-proxy path, kcp-operator VirtualWorkspace) of the virtual
# workspaces served behind the front proxy (charts kcp-access-vw, kcp-mcp-vw).
_VIRTUAL_WORKSPACES = {
    "kcp-access-vw": ("/services/access", "access-vw"),
    "kcp-mcp-vw": ("/services/mcp", "mcp-vw"),
}


def _front_proxy(ctx):
    """Values of kcp.frontProxy, routing to the selected virtual workspaces."""
    values = {"resources": NO_REQUESTS}
    selected = [vw for name, vw in _VIRTUAL_WORKSPACES.items() if ctx.selected(name)]
    if selected:
        # Lists replace on override: repeat the chart's own mappings.
        defaults = read_yaml(CHARTS + "/infra/values.yaml")["kcp"]["frontProxy"]["additionalPathMappings"]
        values["additionalPathMappings"] = defaults + [
            {
                "path": path,
                "backend": "https://{}-virtual-workspace.{}.svc.cluster.local:6443".format(name, NAMESPACE),
                "backend_server_ca": "/etc/kcp/tls/ca/tls.crt",
                "proxy_client_cert": "/etc/kcp-front-proxy/requestheader-client/tls.crt",
                "proxy_client_key": "/etc/kcp-front-proxy/requestheader-client/tls.key",
            }
            for path, name in selected
        ]
    return values


def _selected_only(blob, ctx):
    out = []
    for o in decode_yaml_stream(blob):
        owners = [name for name, owned in INFRA_OWNED.items() if owned(o)]
        if not owners or any([ctx.selected(name) for name in owners]):
            out.append(o)
    return out


def _dex_values():
    """Dex as identity provider of kcp, with the static users of settings.Tiltfile."""
    return {
        "enabled": True,
        "issuer": DEX_ISSUER,
        "gatewayApi": {"hostnames": [BASE_DOMAIN]},
        # The password grant issues tokens without a browser.
        "passwordConnector": "local",
        # Dex v2.46.1 deadlocks its memory storage when it rotates a refresh token;
        # without rotation and inside the reuse interval it returns before that.
        "expiry": {"refreshTokens": {"disableRotation": True, "reuseInterval": "8760h"}},
        "staticPasswords": DEX_USERS,
        "staticClient": {
            "id": DEX_CLIENT_ID,
            "secret": DEX_CLIENT_SECRET,
            "name": "Platform Mesh",
            # Dex compares redirect URIs exactly; this is the portal's callback.
            "redirectURIs": ["https://{}/callback?storageType=none".format(BASE_DOMAIN_PORT)],
        },
    }


def infra_blob(ctx):
    """The rendered chart, reduced to the selected components; applied on first use."""
    if "infra" not in ctx.cache:
        # Second shard, exercised by the e2e tests only.
        shards = [{"name": "triton", "spec": {"resources": NO_REQUESTS}}]
        overrides = {
            # Traefik exposes 8443; public URLs use the NodePort in the contract.
            "gatewayApi": {"listenerPort": GATEWAY_PORT},
            "kcp": {
                "rootShard": {"resources": NO_REQUESTS},
                "shards": shards if ctx.selected("kcp-triton") else [],
                # kcp-operator defaults both to 2; one is enough on a single node.
                "proxy": {"replicas": 1},
                "cacheServer": {"replicas": 1, "resources": NO_REQUESTS},
                "frontProxy": _front_proxy(ctx),
                "etcd": {"resources": NO_REQUESTS, "backup": {"resources": NO_REQUESTS}},
                # The ReBAC authorization webhook is pm-openfga-authz-webhook.
                "webhook": {"enabled": ctx.selected("pm-openfga-authz-webhook")},
            },
            "keycloak": {"operator": {"enabled": ctx.selected("keycloak"), "resources": NO_REQUESTS}},
        }
        if ctx.selected("dex"):
            overrides["dex"] = _dex_values()
            overrides["kcp"]["auth"] = {
                "oidc": {
                    "enabled": True,
                    "issuerUrl": DEX_ISSUER,
                    "clientID": DEX_CLIENT_ID,
                    "groupsClaim": "groups",
                    "usernameClaim": "email",
                }
            }
        blob = profile_chart("infra", NAMESPACE, TEMPLATE_VARS, overrides=overrides)
        objects = _selected_only(blob, ctx)
        blob = encode_yaml_stream(objects)
        if objects:
            k8s_yaml(blob)
        ctx.cache["infra"] = blob
    return ctx.cache["infra"]


def infra_objects(objects=None, **kwargs):
    """Runtime: the objects of the chart the component owns, or those matching the predicate.

    kwargs go to k8s_resource (extra_pod_selectors, pod_readiness).
    """

    def run(c, ctx):
        k8s_resource(
            new_name=c.name,
            objects=select_objects(infra_blob(ctx), objects or INFRA_OWNED[c.name]),
            resource_deps=ctx.deps,
            labels=[c.label],
            **kwargs
        )

    return run
