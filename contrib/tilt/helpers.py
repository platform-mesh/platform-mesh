# Copyright 2026 The Platform Mesh Authors.
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

# helpers.py — chart resolution, kcp module loading, and component hot-reload
# for the Platform Mesh Tilt dev environment.

def load_kcp():
    """Load deploy_kcp() from the kcp repo.

    The static-install module lives upstream in the kcp repo (merged to main).
    Overrides via env:
      KCP_TILT_DIR  — path to a local kcp/contrib/tilt checkout; skips the
                      git fetch entirely (use for offline work / hacking on the
                      module itself).
      KCP_TILT_REPO — git URL   (default: kcp-dev/kcp).
      KCP_TILT_REF  — git ref   (default: main; pin to a tag/release for repro).

    Returns the deploy_kcp function. Uses load_dynamic (not load()) because the
    path is computed and the fetch must run before the load.
    """
    local_dir = os.getenv('KCP_TILT_DIR', '')
    if local_dir:
        path = os.path.join(local_dir, 'kcp_static.Tiltfile')
    else:
        repo = os.getenv('KCP_TILT_REPO', 'https://github.com/kcp-dev/kcp')
        ref = os.getenv('KCP_TILT_REF', 'main')
        dest = '.cache/kcp'
        # Idempotent shallow checkout of the ref (clone first time, fetch after).
        local(
            'if [ -d {d}/.git ]; then git -C {d} fetch -q --depth 1 origin {r} && git -C {d} checkout -q FETCH_HEAD; else git clone -q --depth 1 --branch {r} {u} {d}; fi'.format(
                d=dest, r=ref, u=repo,
            ),
            quiet=True,
            echo_off=True,
        )
        path = os.path.join(dest, 'contrib/tilt/kcp_static.Tiltfile')
    return load_dynamic(path)['deploy_kcp']

def chart_path(name, version, oci_repo, cache_dir='.cache/charts'):
    """Resolve a Platform Mesh Helm chart to a local directory path.

    By default pulls the pinned version from the OCI registry into cache_dir,
    so `tilt up` works from a clone of this monorepo alone (no helm-charts
    checkout). Set HELM_CHARTS_DIR to a local helm-charts checkout to override
    the source for chart development — then charts render from
    $HELM_CHARTS_DIR/charts/<name> live.

    Returns a path suitable for helm(...).
    """
    local_dir = os.getenv('HELM_CHARTS_DIR', '')
    if local_dir:
        return os.path.join(local_dir, 'charts', name)

    dest = os.path.join(cache_dir, name)
    # Idempotent pull: only fetch when the pinned version is not already cached.
    local(
        'test -d {dest} || (mkdir -p {cache} && helm pull {repo}/{name} --version {version} --untar --untardir {cache})'.format(
            dest=dest, cache=cache_dir, repo=oci_repo, name=name, version=version,
        ),
        quiet=True,
        echo_off=True,
    )
    return dest


def component_build(name, path, deps, image, chart, namespace, values=[], helm_set=[], resource_deps=[], objects=[], workload=''):
    """Hot-reload a monorepo operator/service.

    1. compile the component to a linux binary on the host (fast, cached by go)
    2. bake the binary into a thin runtime image, live_update-syncing the
       binary on rebuild instead of a full docker build
    3. deploy the component's production Helm chart with the image overridden
       to the Tilt-built one

    deps: extra source dirs that should trigger a rebuild (shared modules like
    apis/, subroutines/, golang-commons/).
    helm_set: list of "key=value" chart overrides passed as helm --set. Use to
    drop parts of a production chart that don't belong on the local kube cluster
    (e.g. crds.enabled=false to skip the kcp APIExport/APIResourceSchema objects,
    whose CRDs only exist inside kcp workspaces, not the runtime cluster).
    """
    # Paths here resolve relative to THIS Tiltfile's directory (contrib/tilt), so
    # the binary output and runtime image are addressed as ./bin and
    # ./runtime.Dockerfile, while repo-root sources need a ../.. prefix.
    bin_path = './bin/{}'.format(name)
    local_resource(
        'build:{}'.format(name),
        # Build from the repo root so the go.work workspace (apis/, subroutines/,
        # golang-commons/) is in scope, dropping the linux binary into ./bin.
        cmd='cd ../.. && CGO_ENABLED=0 GOOS=linux GOARCH={arch} go build -o contrib/tilt/bin/{name} ./{path}'.format(
            arch=os.getenv('GOARCH', 'arm64'), name=name, path=path,
        ),
        deps=['../../{}'.format(d) for d in [path] + deps],
        labels=['components'],
        allow_parallel=True,
    )
    docker_build(
        ref=image,
        context='./bin',
        dockerfile='./runtime.Dockerfile',
        build_args={'BIN': name},
        only=[name],
        live_update=[sync(bin_path, '/entrypoint')],
    )
    k8s_yaml(helm(
        chart,
        name=name,
        namespace=namespace,
        values=values,
        set=helm_set,
    ))
    # Gate the deployed workload on any prerequisites (e.g. the namespace resource)
    # so a fresh cluster doesn't race "namespace not found". `objects` folds the
    # chart's non-workload objects (cert-manager PKI, ServiceAccount, RBAC) into
    # this resource — otherwise Tilt drops them into its dependency-less catch-all
    # ("uncategorized"), which applies before the namespace and fails on a fresh
    # cluster. `workload` is the actual Deployment/Tilt resource name when the chart
    # doesn't name it after the component (renamed back to `name` for the UI).
    wl = workload if workload else name
    if resource_deps or objects or wl != name:
        if wl != name:
            k8s_resource(wl, new_name=name, objects=objects, resource_deps=resource_deps)
        else:
            k8s_resource(name, objects=objects, resource_deps=resource_deps)
