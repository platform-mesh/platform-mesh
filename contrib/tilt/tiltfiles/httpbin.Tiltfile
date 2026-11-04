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

# HTTPBin e2e fixture: provider workspace, api-syncagent and the example HTTPBin
# operator. The PlatformMesh CR (manifests/platformmesh.yaml) writes the provider
# kubeconfig Secret and binds the export into every account.

load("ext://namespace", "namespace_create")
load("../helpers.py", "TILT_DIR", "chart", "kind_in", "remote_chart", "select_objects", "write_values")
load("./settings.Tiltfile", "BASE_DOMAIN", "CONTEXT", "HTTPBIN_NAMESPACE", "KCP_URL", "NAMESPACE", "PORT", "VERSIONS")

namespace_create(HTTPBIN_NAMESPACE)
k8s_resource(new_name="httpbin-namespace", objects=[HTTPBIN_NAMESPACE + ":namespace"], labels=["e2e"])

api_syncagent = remote_chart(
    "api-syncagent",
    "oci://ghcr.io/platform-mesh/ocm/charts",
    VERSIONS["API_SYNCAGENT_CHART_VERSION"],
    HTTPBIN_NAMESPACE,
    values=[
        write_values(
            "api-syncagent",
            {
                "apiExportEndpointSliceName": "orchestrate.platform-mesh.io",
                "agentName": "kcp-api-syncagent",
                "kcpKubeconfig": "httpbin-kubeconfig",
                "image": {"tag": VERSIONS["API_SYNCAGENT_IMAGE_VERSION"]},
            },
        )
    ],
)
k8s_yaml(api_syncagent)
k8s_resource(
    new_name="api-syncagent-crds",
    objects=select_objects(api_syncagent, kind_in(["CustomResourceDefinition"])),
    labels=["e2e"],
)
k8s_resource(
    "api-syncagent",
    objects=select_objects(api_syncagent, lambda o: o["kind"] != "CustomResourceDefinition"),
    resource_deps=["httpbin-namespace", "export-httpbin-kubeconfig", "example-httpbin-operator"],
    labels=["e2e"],
)

httpbin = chart(
    "example-httpbin-operator",
    HTTPBIN_NAMESPACE,
    values=[
        write_values(
            "example-httpbin-operator",
            {
                "operator": {
                    "args": [
                        "--local-http-route",
                        "--local-http-route-port={}".format(PORT),
                        "--local-http-route-gateway-name=k8sapi-gateway",
                        "--local-http-route-gateway-namespace=" + NAMESPACE,
                        "--domain=httpbin.services." + BASE_DOMAIN,
                    ]
                },
                "apiSyncagent": {
                    "rbac": {"enable": True},
                    "publishedResources": {"enable": True},
                    "serviceAccount": {"name": "api-syncagent", "namespace": HTTPBIN_NAMESPACE},
                },
            },
        )
    ],
)
k8s_yaml(httpbin)
k8s_resource(
    "example-httpbin-operator",
    objects=select_objects(httpbin, lambda o: True),
    resource_deps=["httpbin-namespace", "api-syncagent-crds", "gateway"],
    labels=["e2e"],
)

# Not after platformmesh-ready: the PlatformMesh binds this export into every
# account, so platform-mesh-operator only gets Ready once the export exists.
BOOTSTRAP = os.path.join(TILT_DIR, "hack/httpbin-provider-bootstrap.sh")
local_resource(
    "httpbin-provider-bootstrap",
    labels=["e2e"],
    allow_parallel=True,
    resource_deps=["kcp-ready", "kcp-gateway-ready", "export-kcp-admin-kubeconfig"],
    deps=[os.path.join(TILT_DIR, p) for p in ["manifests/httpbin-provider", "manifests/httpbin-workspace.yaml"]] + [BOOTSTRAP],
    env={"KCP_URL": KCP_URL},
    dir=TILT_DIR,
    cmd=[BOOTSTRAP],
)
# apiexportpolicies exist in root:orgs once the kcp setup has finished.
local_resource(
    "httpbin-apiexportpolicy",
    labels=["e2e"],
    allow_parallel=True,
    resource_deps=["platformmesh-ready", "httpbin-provider-bootstrap"],
    deps=[os.path.join(TILT_DIR, "manifests/httpbin-apiexportpolicy.yaml")],
    dir=TILT_DIR,
    cmd="kubectl --kubeconfig .secret/kcp-admin.kubeconfig --server {}/clusters/root:orgs apply -f manifests/httpbin-apiexportpolicy.yaml".format(KCP_URL),
)
# Written by platform-mesh-operator (extraProviderConnections); the e2e tests read it.
EXPORT = os.path.join(TILT_DIR, "hack/export-secret.sh")
local_resource(
    "export-httpbin-kubeconfig",
    labels=["e2e"],
    allow_parallel=True,
    resource_deps=["platformmesh-ready"],
    deps=[EXPORT],
    env={"KUBE_CONTEXT": CONTEXT, "NAMESPACE": HTTPBIN_NAMESPACE},
    dir=TILT_DIR,
    cmd=[EXPORT, "httpbin-kubeconfig", "kubeconfig", ".secret/httpbin-provider.kubeconfig"],
)
local_resource(
    "httpbin-api-ready",
    labels=["e2e"],
    allow_parallel=True,
    resource_deps=["api-syncagent", "httpbin-apiexportpolicy"],
    dir=TILT_DIR,
    cmd="""kubectl --kubeconfig .secret/httpbin-provider.kubeconfig \
                   wait apiexport/orchestrate.platform-mesh.io \
                   --for=jsonpath='{.spec.resources[?(@.name=="httpbins")].schema}' --timeout=5m""",
)
