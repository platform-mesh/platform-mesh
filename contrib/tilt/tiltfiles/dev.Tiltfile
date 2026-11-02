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

# Dev: credentials exported to the host (.secret/ is gitignored).

load("../helpers.py", "TILT_DIR")
load("./settings.Tiltfile", "CONTEXT", "NAMESPACE")

EXPORT = os.path.join(TILT_DIR, "hack/export-secret.sh")
EXPORT_ENV = {"KUBE_CONTEXT": CONTEXT, "NAMESPACE": NAMESPACE}
local_resource(
    "export-ca",
    labels=["dev"],
    resource_deps=["domain-certificate"],
    allow_parallel=True,
    deps=[EXPORT],
    env=EXPORT_ENV,
    dir=TILT_DIR,
    cmd=[EXPORT, "domain-certificate", "ca.crt", ".secret/ca.crt", "certificate/platform-mesh-gateway"],
)
local_resource(
    "export-kcp-ca",
    labels=["dev"],
    resource_deps=["kcp-ready"],
    allow_parallel=True,
    deps=[EXPORT],
    env=EXPORT_ENV,
    dir=TILT_DIR,
    cmd=[EXPORT, "root-ca", "tls.crt", ".secret/kcp-root-ca.crt"],
)
local_resource(
    "export-kcp-admin-kubeconfig",
    labels=["dev"],
    resource_deps=["kcp-ready"],
    allow_parallel=True,
    deps=[EXPORT],
    env=EXPORT_ENV,
    dir=TILT_DIR,
    cmd=[EXPORT, "kubeconfig-kcp-admin", "kubeconfig", ".secret/kcp-admin.kubeconfig"],
)
