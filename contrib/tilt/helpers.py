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

# helpers.py: chart rendering, kind-profile values and component hot-reload
# for the Platform Mesh Tilt dev environment. Relative paths given to these
# helpers resolve against TILT_DIR (contrib/tilt), whichever Tiltfile calls them.

# Syncing a new binary into a container does not restart the process already
# running from the old one, and Tilt's built-in restart_container() step only
# works on the docker runtime; kind runs containerd. This extension wraps the
# entrypoint in `entr` and rewrites a trigger file as the last live_update step,
# so the operator actually re-execs on every sync.
load('ext://restart_process', 'docker_build_with_restart')

# `go env` honours a GOARCH override from the environment.
BUILD_GOARCH = str(local('go env GOARCH', quiet=True, echo_off=True)).strip()

# os.getcwd() during load is this file's directory; Tilt otherwise resolves
# relative paths against the Tiltfile that called the helper.
TILT_DIR = os.getcwd()
REPO = os.path.abspath(os.path.join(TILT_DIR, '..', '..'))
CHARTS = os.path.join(REPO, 'charts')
KIND_PROFILE = CHARTS + '/platform-mesh-operator/files/kind-profile.yaml'
IMAGE_PREFIX = 'ghcr.io/platform-mesh/platform-mesh/'


def chart_deps():
    """Resolve charts/*/Chart.yaml dependencies (charts/*/charts/) once per eval.

    helm() runs `helm template` only and does not fetch dependencies; every
    chart here depends on file://../common. Re-runs the resolver when a chart
    has no charts/ dir or a file:// dependency changed after it was packaged
    into charts/<dep>-<version>.tgz.
    Needs network the first time (OCI dependencies of the prerequisites).
    """
    watch_file(CHARTS + '/common')
    checks = []
    for name in str(local('ls ' + CHARTS, quiet=True, echo_off=True)).split():
        chart_yaml = '{}/{}/Chart.yaml'.format(CHARTS, name)
        if not os.path.exists(chart_yaml):
            continue
        deps = read_yaml(chart_yaml).get('dependencies', [])
        if not deps:
            continue
        chart_dir = '{}/{}'.format(CHARTS, name)
        checks.append('[ -d {dir}/charts ] || echo {name}'.format(dir=chart_dir, name=name))
        for dep in deps:
            if not dep['repository'].startswith('file://'):
                continue
            dep_dir = os.path.join(chart_dir, dep['repository'][len('file://'):])
            tgz = '{}/charts/{}-{}.tgz'.format(chart_dir, dep['name'], dep['version'])
            checks.append('[ -f {tgz} ] && [ -n "$(find {dep} -type f -newer {tgz} -print -quit)" ] && echo {name}'.format(
                tgz=tgz, dep=dep_dir, name=name))
    stale = str(local('\n'.join(checks) + '\ntrue', quiet=True, echo_off=True)).strip()
    if stale:
        print('chart dependencies stale ({}), resolving'.format(' '.join(sorted(set(stale.split())))))
        local(REPO + '/hack/charts/resolve-dependencies.sh', quiet=True)


def _deep_merge(base, overrides):
    """Return base with overrides merged in; nested dicts merge, anything else replaces."""
    out = dict(base)
    for k, v in overrides.items():
        if type(v) == 'dict' and type(out.get(k)) == 'dict':
            out[k] = _deep_merge(out[k], v)
        else:
            out[k] = v
    return out


def profile_values(service, tvars, overrides={}):
    """Helm values of one service from the kind-profile, templated and overridden.

    tvars:     {{ .name }} substitutions (baseDomain, baseDomainPort, ...),
               plus gatewayClassName/gatewayClusterIP/port for the
               __PLACEHOLDER__ tokens the PMO chart replaces
    overrides: deep-merged on top of the profile's values
    """
    profile = str(decode_yaml(read_file(KIND_PROFILE))['data']['profile.yaml'])
    for k, v in tvars.items():
        profile = profile.replace('{{ .%s }}' % k, v)
    profile = profile.replace('__GATEWAY_CLASS_NAME__', tvars['gatewayClassName'])
    profile = profile.replace('__EXTERNAL_PORT__', tvars['port'])
    profile = profile.replace('__GATEWAY_SERVICE_CLUSTER_IP__', tvars['gatewayClusterIP'])
    services = decode_yaml(profile)['components']['services']
    if service not in services:
        fail('{} is not a service in {}'.format(service, KIND_PROFILE))
    values = services[service].get('values', {})
    # Every hostname resolves to the node via sslip.io; the profile's aliases
    # would pin kcp names to a gateway service ClusterIP instead.
    if 'hostAliases' in values:
        values['hostAliases'] = {'enabled': False}
    return _deep_merge(values, overrides)


def write_yaml(path, obj):
    """Write obj as YAML to TILT_DIR/path if the content changed; returns the absolute path."""
    path = os.path.join(TILT_DIR, path)
    local('mkdir -p {d} && cat > {p}.tmp && (cmp -s {p}.tmp {p} && rm {p}.tmp || mv {p}.tmp {p})'.format(d=os.path.dirname(path), p=path),
          stdin=encode_yaml(obj), quiet=True, echo_off=True)
    return path


def write_values(service, values):
    """Write values to .cache/values/<service>.yaml if changed; returns the path."""
    return write_yaml('.cache/values/{}.yaml'.format(service), values)


def profile_values_file(service, tvars, overrides={}):
    """Write the kind-profile values of service (see profile_values); returns the path."""
    return write_values(service, profile_values(service, tvars, overrides))


def render(path, vars):
    """Substitute __KEY__ placeholders in the manifest at TILT_DIR/path; returns a blob.

    vars: {KEY: value}, KEY without the surrounding underscores
    """
    out = str(read_file(os.path.join(TILT_DIR, path)))
    for k, v in vars.items():
        out = out.replace('__{}__'.format(k), str(v))
    return blob(out)


# Kinds Tilt turns into resources on its own, or attaches to them (Services
# matching a workload's pod labels); naming them in k8s_resource(objects=) fails.
_WORKLOAD_KINDS = ['Deployment', 'StatefulSet', 'DaemonSet', 'Job', 'CronJob', 'Pod', 'Service']


def select_objects(blob, pred):
    """Selectors ('name:kind') of the non-workload objects in blob for which pred(obj) is true."""
    out = []
    for o in decode_yaml_stream(blob):
        if not o or o['kind'] in _WORKLOAD_KINDS:
            continue
        if pred(o):
            out.append('{}:{}'.format(o['metadata']['name'].replace(':', '\\:'), o['kind']))
    return out


def named(sub):
    """Predicate: metadata.name contains sub."""
    return lambda o: sub in o['metadata']['name']


def suffixed(suffix):
    """Predicate: metadata.name ends with suffix."""
    return lambda o: o['metadata']['name'].endswith(suffix)


def kind_in(kinds):
    """Predicate: kind is one of kinds."""
    return lambda o: o['kind'] in kinds


def _pod_spec(o):
    """Pod spec of a workload object, or None."""
    if o['kind'] == 'Pod':
        return o.get('spec')
    if o['kind'] == 'CronJob':
        return o.get('spec', {}).get('jobTemplate', {}).get('spec', {}).get('template', {}).get('spec')
    return o.get('spec', {}).get('template', {}).get('spec')


def _strip_resources(o):
    """Drop resources from every container of workload o, in place.

    The charts' production requests add up to more CPU than a small kind node
    has. Limits go too: Kubernetes defaults missing requests to the limits.
    """
    spec = _pod_spec(o)
    if not spec:
        return
    for c in spec.get('containers', []) + spec.get('initContainers', []):
        c.pop('resources', None)


def _release(blob, namespace):
    """Post-process `helm template` output the way `helm install` would; returns a blob.

    - objects without metadata.namespace get the release namespace; objects
      that name another one (cert-manager's leader election in kube-system)
      keep it, which ext://namespace's namespace_inject would clobber
    - repeated objects (same apiVersion, kind, namespace, name) are dropped:
      --include-crds emits a subchart's crds/ directory next to the CRD
      templates of a sibling subchart (traefik and traefik-crds)
    - workloads lose their resources (see _strip_resources)
    """
    seen = {}
    out = []
    for o in decode_yaml_stream(blob):
        if not o:
            continue
        meta = o.setdefault('metadata', {})
        if not meta.get('namespace'):
            meta['namespace'] = namespace
        key = '{}/{}/{}/{}'.format(o.get('apiVersion'), o.get('kind'), meta['namespace'], meta.get('name'))
        if key in seen:
            continue
        seen[key] = True
        if o['kind'] in _WORKLOAD_KINDS:
            _strip_resources(o)
        out.append(o)
    return encode_yaml_stream(out)


def chart(name, namespace, values=[], set=[]):
    """Render charts/<name> into namespace; returns a blob for k8s_yaml().

    helm() watches the chart directory; chart_deps() must have run.
    """
    return _release(helm('{}/{}'.format(CHARTS, name), name=name, namespace=namespace, values=values, set=set), namespace)


def profile_chart(name, namespace, tvars, overrides={}):
    """Render charts/<name> with its kind-profile values; returns a blob for k8s_yaml().

    tvars, overrides: see profile_values
    """
    return chart(name, namespace, values=[profile_values_file(name, tvars, overrides)])


def remote_chart(name, repo, version, namespace, values=[], set=[]):
    """Render a third-party chart pulled once into .cache/charts; returns a blob.

    repo:    oci:// repository or https:// chart repository holding the chart,
             the same source the chart's third_party constructor names
    version: exact chart version
    """
    cache = os.path.join(TILT_DIR, '.cache/charts/{}-{}'.format(name, version))
    path = '{}/{}'.format(cache, name)
    if not os.path.exists(path + '/Chart.yaml'):
        if repo.startswith('oci://'):
            ref = '{}/{}'.format(repo, name)
        else:
            ref = '{} --repo {}'.format(name, repo)
        local('rm -rf {c} && mkdir -p {c} && helm pull {ref} --version {v} --untar --untardir {c}'.format(
            c=cache, ref=ref, v=version), quiet=True)
    return _release(helm(path, name=name, namespace=namespace, values=values, set=set), namespace)


def writable_rootfs(blob, image):
    """Drop readOnlyRootFilesystem from containers running image; returns a blob.

    charts/common hardcodes readOnlyRootFilesystem=true. live_update replaces
    /app/entrypoint in place and the restart wrapper writes /tmp/.restart-proc,
    both impossible on a read-only root. Dev-only, applied where Tilt also
    rewrites the image tag.
    """
    out = []
    for o in decode_yaml_stream(blob):
        if not o:
            continue
        pod = o.get('spec', {}).get('template', {}).get('spec', {})
        for c in pod.get('containers', []) + pod.get('initContainers', []):
            if c.get('image', '').startswith(image + ':') and 'securityContext' in c:
                c['securityContext'].pop('readOnlyRootFilesystem', None)
        out.append(o)
    return encode_yaml_stream(out)


def service(name, namespace, tvars, path=None, deps=[], overrides={}, workloads=[], resource_deps=[], labels=['platform-mesh']):
    """Deploy charts/<name> with its kind-profile values, hot-reloaded when path is given.

    path:          module directory relative to the repo root; None for
                   services whose image is not built here (UIs)
    deps:          extra repo-root-relative source dirs that trigger a rebuild
    overrides:     Tilt-only values merged over the profile's
    workloads:     Tilt's names for the chart's workloads if not [<name>];
                   the first one is shown as <name> and carries the objects
    resource_deps: added to the implicit platform-mesh-operator dependency
    """
    blob = profile_chart(name, namespace, tvars, overrides)
    if path:
        component_binary(name, path, deps, IMAGE_PREFIX + name, labels=labels)
        blob = writable_rootfs(blob, IMAGE_PREFIX + name)
    k8s_yaml(blob)
    # RBAC, certificates, routes: everything the chart renders besides pods.
    objects = select_objects(blob, lambda o: True)
    workloads = workloads or [name]
    resource_deps = ['platform-mesh-operator'] + resource_deps
    if workloads[0] != name:
        k8s_resource(workloads[0], new_name=name, objects=objects, resource_deps=resource_deps, labels=labels)
    else:
        k8s_resource(name, objects=objects, resource_deps=resource_deps, labels=labels)
    for workload in workloads[1:]:
        k8s_resource(workload, resource_deps=resource_deps, labels=labels)


def component_binary(name, path, deps, image, extra_dirs={}, labels=['platform-mesh']):
    """Compile a component to a linux binary and bake it into the thin runtime image.

    name:       component name, also the ./bin/<name> output directory
    path:       module directory relative to the repo root (holds main.go)
    deps:       extra repo-root-relative source dirs that trigger a rebuild
    image:      image ref the chart renders; Tilt rewrites its tag
    extra_dirs: {repo-root-relative dir: path under /app} payload the binary
                reads at runtime (none today; manifests and templates are embedded)
    """
    # Each component gets its own context directory holding a single file named
    # `entrypoint`, rather than one shared bin/ filtered with only=[name]. The
    # restart_process extension builds two images and forwards the same kwargs to
    # both, so a context-relative `only`/`build_args` meant for the first would
    # leak into the second (whose context is the extension's own directory) and
    # match nothing. A per-component context needs neither.
    bin_dir = os.path.join(TILT_DIR, 'bin', name)
    # Build from the repo root so the go.work workspace (apis/, subroutines/,
    # golang-commons/) is in scope.
    cmd = 'mkdir -p {bin} && CGO_ENABLED=0 GOOS=linux GOARCH={arch} go build -o {bin}/entrypoint ./{path}'.format(
        bin=bin_dir, arch=BUILD_GOARCH, path=path)
    for src, dst in extra_dirs.items():
        cmd += ' && mkdir -p {bin}/{dst} && rsync -a --delete {src}/ {bin}/{dst}/'.format(bin=bin_dir, src=src, dst=dst)
    local_resource(
        'build:{}'.format(name),
        cmd=cmd,
        dir=REPO,
        deps=[os.path.join(REPO, d) for d in [path] + deps],
        labels=labels,
        allow_parallel=True,
    )
    # entrypoint as a LIST, not a string: the extension runs a string entrypoint
    # under `sh -c`, which swallows the container `args` the charts set (they pass
    # args only and inherit the image entrypoint). A list is appended to verbatim.
    docker_build_with_restart(
        ref=image,
        context=bin_dir,
        dockerfile=os.path.join(TILT_DIR, 'runtime.Dockerfile'),
        entrypoint=['/app/entrypoint'],
        live_update=[sync(bin_dir, '/app')],
    )
