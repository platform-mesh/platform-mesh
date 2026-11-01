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

# helpers.py — chart resolution and component hot-reload for the Platform Mesh
# Tilt dev environment.


# Syncing a new binary into a container does not restart the process already
# running from the old one, and Tilt's built-in restart_container() step only
# works on the docker runtime — kind runs containerd. This extension wraps the
# entrypoint in `entr` and rewrites a trigger file as the last live_update step,
# so the operator actually re-execs on every sync.
load('ext://restart_process', 'docker_build_with_restart')

BUILD_GOARCH = os.getenv('GOARCH', '') or str(local('go env GOARCH', quiet=True, echo_off=True)).strip()

def chart_path(name, version=None, oci_repo=None, cache_dir='.cache/charts'):
    """Resolve a Platform Mesh Helm chart to a local directory path.

    Charts live in this repository under charts/<name>, so by default that
    directory is used and charts render live. Set HELM_CHARTS_DIR to point at
    a different checkout. If TILT_CHARTS_FROM_OCI is set and version/oci_repo
    are given, the pinned version is pulled from the OCI registry into
    cache_dir instead.

    Returns a path suitable for helm(...).
    """
    local_dir = os.getenv('HELM_CHARTS_DIR', '') or (
        '' if os.getenv('TILT_CHARTS_FROM_OCI', '') else '.')
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


def component_binary(name, path, deps, image, labels=['components']):
    """Compile a component to a linux binary and bake it into the thin runtime image.

    Split out from component_build() so the build half stays separable from the
    deploy half — and PUBLIC, because not every component deploys from a Helm
    chart. A component whose manifests are kustomize (platform-mesh-deployer)
    calls this directly and then does its own k8s_yaml(kustomize(...)); the build,
    the live_update sync and the image ref are identical either way.
    """
    # Paths here resolve relative to THIS Tiltfile's directory (contrib/tilt), so
    # the binary output and runtime image are addressed as ./bin and
    # ./runtime.Dockerfile, while repo-root sources need a ../.. prefix.
    #
    # Each component gets its OWN context directory holding a single file named
    # `entrypoint`, rather than one shared ./bin filtered with only=[name]. The
    # restart_process extension builds two images and forwards the same kwargs to
    # both, so a context-relative `only`/`build_args` meant for the first would
    # leak into the second (whose context is the extension's own directory) and
    # match nothing. A per-component context needs neither.
    bin_dir = './bin/{}'.format(name)
    bin_path = '{}/entrypoint'.format(bin_dir)
    local_resource(
        'build:{}'.format(name),
        # Build from the repo root so the go.work workspace (apis/, subroutines/,
        # golang-commons/) is in scope, dropping the linux binary into ./bin.
        # The `rm -f` clears the pre-per-component-context layout, where ./bin/<name>
        # was the binary itself and would block mkdir of the directory. Guarded by
        # the -d test because `rm -f` on an existing directory exits non-zero, which
        # would fail every rebuild after the first.
        # (Subshell, not a `{ ...; }` group: braces would be eaten by .format().)
        cmd='cd ../.. && ( [ -d contrib/tilt/bin/{name} ] || rm -f contrib/tilt/bin/{name} ) && mkdir -p contrib/tilt/bin/{name} && CGO_ENABLED=0 GOOS=linux GOARCH={arch} go build -o contrib/tilt/bin/{name}/entrypoint ./{path}'.format(
            arch=BUILD_GOARCH, name=name, path=path,
        ),
        deps=['../../{}'.format(d) for d in [path] + deps],
        labels=labels,
        allow_parallel=True,
    )
    # entrypoint as a LIST, not a string: the extension runs a string entrypoint
    # under `sh -c`, which swallows the container `args` the charts set (they pass
    # args only and inherit the image entrypoint). A list is appended to verbatim.
    docker_build_with_restart(
        ref=image,
        context=bin_dir,
        dockerfile='./runtime.Dockerfile',
        entrypoint=['/app/entrypoint'],
        live_update=[sync(bin_path, '/app/entrypoint')],
    )


def component_build(name, path, deps, image, chart, namespace, values=[], helm_set=[], resource_deps=[], objects=[], workload='', labels=['components']):
    """Hot-reload a monorepo operator/service.

    1. compile the component to a linux binary on the host (fast, cached by go)
    2. bake the binary into a thin runtime image, live_update-syncing the
       binary on rebuild instead of a full docker build
    3. deploy the component's production Helm chart with the image overridden
       to the Tilt-built one

    deps: extra source dirs that should trigger a rebuild (shared modules like
    apis/, subroutines/, golang-commons/).
    labels: Tilt UI grouping for both the build and the deployed workload. Pass the
    feature name (e.g. ['auth']) so a profile's resources stay together.
    helm_set: list of "key=value" chart overrides passed as helm --set. Use to
    drop parts of a production chart that don't belong on the local kube cluster
    (e.g. crds.enabled=false to skip the kcp APIExport/APIResourceSchema objects,
    whose CRDs only exist inside kcp workspaces, not the runtime cluster).
    """
    component_binary(name, path, deps, image, labels)
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
    # Called unconditionally: without it the workload lands in Tilt's UI with no
    # label at all, in Tilt's dependency-less catch-all group.
    wl = workload if workload else name
    if wl != name:
        k8s_resource(wl, new_name=name, objects=objects, resource_deps=resource_deps, labels=labels)
    else:
        k8s_resource(name, objects=objects, resource_deps=resource_deps, labels=labels)
