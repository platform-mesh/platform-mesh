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

# The platform-mesh-prerequisites chart: cert-manager, traefik with the Gateway
# API CRDs, cnpg, keycloak-operator, etcd-druid and the local domain
# certificate. One chart shared by several components: rendered once per run
# with only the selected parts enabled, each component claims its objects.

load("./helpers.py", "REPO", "chart", "select_objects")
load("./settings.Tiltfile", "BASE_DOMAIN", "FRONTEND_CORS", "GATEWAY_PORT", "NAMESPACE", "PORT")

# Workloads of the chart are named <release>-<subchart>.
PREREQ_RELEASE = "platform-mesh-prerequisites-"

# Component -> the chart value that enables its part.
_ENABLED = {
    "cert-manager": "cert-manager.enabled",
    "traefik": "traefik.enabled",
    "cnpg": "cnpg.enabled",
    "keycloak-operator": "keycloak-operator.enabled",
    "etcd-druid": "etcd-druid.enabled",
    "domain-certificate": "localCertificates.enabled",
}


def prereq_blob(ctx):
    """The rendered chart; applied on first use."""
    if "prereq" not in ctx.cache:
        enabled = ["{}={}".format(value, str(ctx.selected(name)).lower()) for name, value in _ENABLED.items()]
        blob = chart(
            "platform-mesh-prerequisites",
            NAMESPACE,
            # Opt-in Traefik CORS middleware for a frontend run outside the cluster.
            values=[os.path.join(REPO, "installation/frontend-dev-cors-values.yaml")] if FRONTEND_CORS else [],
            set=enabled
            + [
                "flux2.enabled=false",
                "ocm-k8s-toolkit.enabled=false",
                # Gateway API and Traefik CRDs.
                "traefik-crds.enabled=" + str(ctx.selected("traefik")).lower(),
                "keycloak-operator.watchNamespaces=" + NAMESPACE,
                "traefik.service.type=NodePort",
                "traefik.service.spec.type=NodePort",
                "traefik.ports.websecure.port={}".format(GATEWAY_PORT),
                "traefik.ports.websecure.exposedPort={}".format(GATEWAY_PORT),
                "traefik.ports.websecure.nodePort={}".format(PORT),
                "localCertificates.dnsNames={{{},*.{},*.services.{}}}".format(BASE_DOMAIN, BASE_DOMAIN, BASE_DOMAIN),
            ],
        )
        k8s_yaml(blob)
        ctx.cache["prereq"] = blob
    return ctx.cache["prereq"]


def prereq_workload(workload, objects):
    """Runtime: one workload of the chart, shown as <name>, with the objects matching the predicate."""

    def run(c, ctx):
        k8s_resource(
            workload,
            new_name=c.name,
            objects=select_objects(prereq_blob(ctx), objects),
            resource_deps=ctx.deps,
            labels=[c.label],
        )

    return run


def prereq_objects(objects):
    """Runtime: the objects of the chart matching the predicate, no workload."""

    def run(c, ctx):
        k8s_resource(
            new_name=c.name,
            objects=select_objects(prereq_blob(ctx), objects),
            resource_deps=ctx.deps,
            labels=[c.label],
        )

    return run
