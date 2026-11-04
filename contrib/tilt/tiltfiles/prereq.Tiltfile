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

# Prerequisites: namespace, cert-manager, gateway-api CRDs, traefik,
# cnpg, keycloak-operator, etcd-druid, OCM CRDs.

load("ext://namespace", "namespace_create")
load("../helpers.py", "chart", "kind_in", "named", "select_objects", "suffixed")
load("./settings.Tiltfile", "BASE_DOMAIN", "KUBECTL", "NAMESPACE", "PORT")

namespace_create(NAMESPACE)
k8s_resource(new_name="namespace", objects=[NAMESPACE + ":namespace"], labels=["prereq"])

prereq = chart(
    "platform-mesh-prerequisites",
    NAMESPACE,
    set=[
        "flux2.enabled=false",
        "ocm-k8s-toolkit.enabled=false",
        "keycloak-operator.watchNamespaces=" + NAMESPACE,
        "traefik.enabled=true",
        "traefik.service.type=NodePort",
        "traefik.service.spec.type=NodePort",
        "traefik.ports.websecure.exposedPort=8443",
        "traefik.ports.websecure.nodePort={}".format(PORT),
        "localCertificates.enabled=true",
        "localCertificates.dnsNames={{{},*.{},*.services.{}}}".format(BASE_DOMAIN, BASE_DOMAIN, BASE_DOMAIN),
    ],
)
k8s_yaml(prereq)

PREREQ_RELEASE = "platform-mesh-prerequisites-"
is_traefik_gateway = kind_in(["Gateway", "GatewayClass"])
is_traefik = lambda o: (
    (named("traefik")(o) and not suffixed("traefik.io")(o) and not is_traefik_gateway(o)) or o["kind"] == "IngressClass"
)
is_cnpg = lambda o: named("cnpg")(o) or named("cloudnative-pg")(o)
k8s_resource(
    new_name="gateway-api-crds", objects=select_objects(prereq, named(".gateway.networking.")), labels=["prereq"]
)
k8s_resource(new_name="traefik-crds", objects=select_objects(prereq, suffixed("traefik.io")), labels=["prereq"])
k8s_resource(
    PREREQ_RELEASE + "cert-manager",
    new_name="cert-manager",
    objects=select_objects(prereq, named("cert-manager")),
    resource_deps=["namespace"],
    labels=["prereq"],
)
k8s_resource(
    PREREQ_RELEASE + "cert-manager-cainjector",
    new_name="cert-manager-cainjector",
    resource_deps=["namespace"],
    labels=["prereq"],
)
k8s_resource(
    PREREQ_RELEASE + "cert-manager-webhook",
    new_name="cert-manager-webhook",
    resource_deps=["namespace"],
    labels=["prereq"],
)
k8s_resource(
    PREREQ_RELEASE + "cert-manager-startupapicheck",
    new_name="cert-manager-startupapicheck",
    resource_deps=["cert-manager", "cert-manager-webhook", "cert-manager-cainjector"],
    labels=["prereq"],
)
k8s_resource(
    PREREQ_RELEASE + "traefik",
    new_name="traefik",
    objects=select_objects(prereq, is_traefik),
    resource_deps=["namespace", "gateway-api-crds", "traefik-crds"],
    labels=["prereq"],
)
# Applied once traefik is up: right after gateway-api-crds the Gateway kinds are
# not always discoverable yet and the apply fails with "no matches for kind".
k8s_resource(
    new_name="traefik-gateway",
    objects=select_objects(prereq, is_traefik_gateway),
    resource_deps=["traefik"],
    labels=["prereq"],
)
k8s_resource(
    PREREQ_RELEASE + "cloudnative-pg",
    new_name="cnpg",
    objects=select_objects(prereq, is_cnpg),
    resource_deps=["namespace"],
    labels=["prereq"],
)
k8s_resource(
    PREREQ_RELEASE + "keycloak-operator",
    new_name="keycloak-operator",
    objects=select_objects(prereq, named("keycloak")),
    resource_deps=["namespace"],
    labels=["prereq"],
)
k8s_resource(
    "etcd-druid", objects=select_objects(prereq, named("druid")), resource_deps=["namespace"], labels=["prereq"]
)

# startupapicheck is a Job that issues a test Certificate: it is the only
# signal that cainjector has patched the webhook CA and Issuers can be applied.
k8s_resource(
    new_name="domain-certificate",
    objects=select_objects(prereq, kind_in(["Issuer", "Certificate"])),
    resource_deps=["cert-manager-startupapicheck"],
    labels=["prereq"],
)

# Tilt reports the Certificates as ready once applied; wait for cert-manager to
# issue them, and on failure print why (e.g. IncorrectIssuer when a stale
# Certificate from an older install writes to the same Secret).
DOMAIN_CERTIFICATES = "certificate/platform-mesh-gateway-ca certificate/platform-mesh-gateway"
local_resource(
    "domain-certificate-ready",
    labels=["prereq"],
    resource_deps=["domain-certificate"],
    allow_parallel=True,
    cmd=KUBECTL
    + " wait "
    + DOMAIN_CERTIFICATES
    + " --for=condition=Ready --timeout=5m || { "
    + KUBECTL
    + " get "
    + DOMAIN_CERTIFICATES
    + " -o jsonpath='{range .items[*]}{.metadata.name}: {.status.conditions[?(@.type==\"Ready\")].reason}"
    + " {.status.conditions[?(@.type==\"Ready\")].message}{\"\\n\"}{end}' >&2; exit 1; }",
)

# PMO watches delivery.ocm.software Resources at startup whatever the profile
# says, so the OCM CRDs must exist; the controller behind them is not needed.
ocm_crds = chart(
    "platform-mesh-prerequisites",
    NAMESPACE,
    set=[
        "{}.enabled={}".format(dep, dep == "ocm-k8s-toolkit")
        for dep in [
            "flux2",
            "ocm-k8s-toolkit",
            "cert-manager",
            "gateway-api-crds",
            "traefik",
            "traefik-crds",
            "cnpg",
            "keycloak-operator",
            "etcd-druid",
        ]
    ],
)

ocm_crds = encode_yaml_stream([o for o in decode_yaml_stream(ocm_crds) if o["kind"] == "CustomResourceDefinition"])
k8s_yaml(ocm_crds)
k8s_resource(new_name="ocm-crds", objects=select_objects(ocm_crds, lambda o: True), labels=["prereq"])
