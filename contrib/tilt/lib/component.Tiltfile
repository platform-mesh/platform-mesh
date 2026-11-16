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

# The component contract of the Tilt environment.
#
# A component is one deployable unit with one Tiltfile: next to its code
# (operators/<name>/Tiltfile, services/<name>/Tiltfile) or, for everything
# without a source directory in this repository, in
# contrib/tilt/components/<name>/Tiltfile. The file declares the component and
# deploys nothing by itself:
#
#     COMPONENT = component(
#         name    = "iam-service",
#         group   = "iam",
#         needs   = ["platform-mesh-operator"],
#         runtime = go_service(path="services/iam-service"),
#     )
#
# The root Tiltfile loads every declaration, selects the components asked for
# and calls deploy(). This library then owns
#
#   runtime    how the component is built and deployed (the constructors below,
#              or any function(c, ctx) for the irregular ones)
#   readiness  the one resource that means "this component can be depended
#              on": the resource the runtime ends with, or the <name>-ready
#              gate generated from ready=[...]
#   deps       needs is the only dependency list: it selects the components
#              that are deployed along and turns into resource_deps on their
#              ready resources. No component names a foreign resource.

load("./helpers.py", "TILT_DIR", "profile_values_file", "remote_chart", "select_objects", "service")
load("./settings.Tiltfile", "CONTEXT", "GO_DEPS", "KUBECTL", "NAMESPACE", "TEMPLATE_VARS", "third_party_version")


def component(name, group, needs=[], alongside=[], runtime=None, ready=[]):
    """Declare a component.

    name:      component name, also the name of its main Tilt resource
    group:     label in the Tilt UI
    needs:     components that must be ready before this one is deployed
    alongside: components that must be deployed too, in no particular order
               (iam-service is pointless without Keycloak, but does not wait
               for it)
    runtime:   function(c, ctx) creating the Tilt resources, see the
               constructors below. It returns the name of the resource that
               marks the component deployed if that is not <name>. None for a
               component that is only a readiness gate over its needs.
    ready:     shell commands that all have to succeed once the runtime is up,
               see wait(); an entry may be a function(ctx) returning the
               command. Generates the resource <name>-ready.

    ctx, handed to runtime and ready functions:
      ctx.deps            ready resources of needs, for resource_deps
      ctx.ready(name)     ready resource of one of needs
      ctx.deployed(name)  the resource one of needs is deployed with, for the
                          rare component that starts next to a need instead
                          of after it
      ctx.selected(name)  whether a component is part of this run
      ctx.cache           dict shared by all components of this run
    """
    if not runtime and not ready:
        fail("component {}: needs a runtime, ready conditions or both".format(name))
    return struct(name=name, group=group, needs=needs, alongside=alongside, runtime=runtime, ready=ready)


# --- readiness


def wait(target, condition, timeout="5m", kubectl=KUBECTL):
    """Ready condition: `kubectl wait <target> --for=<condition>` in the platform namespace."""
    return "{} wait {} --for={} --timeout={}".format(kubectl, target, condition, timeout)


def gate(name, cmds, resource_deps, group):
    """A resource that turns green once every command has succeeded."""
    local_resource(
        name,
        labels=[group],
        resource_deps=resource_deps,
        allow_parallel=True,
        dir=TILT_DIR,
        cmd=" && ".join(cmds),
    )


# --- runtime constructors


def go_service(path, workloads=[], overrides={}, deps=GO_DEPS):
    """A Go component of this repository: compiled on the host, hot-reloaded
    into the thin runtime image and deployed from charts/<name> with its
    kind-profile values.

    path:      module directory relative to the repo root
    workloads: Tilt's names for the chart's workloads if not [<name>]
    overrides: Tilt-only values merged over the profile's
    deps:      extra repo-root-relative source dirs that trigger a rebuild
    """

    def run(c, ctx):
        service(
            c.name,
            NAMESPACE,
            TEMPLATE_VARS,
            path=path,
            deps=deps,
            overrides=overrides,
            workloads=workloads,
            resource_deps=ctx.deps,
            labels=[c.group],
        )

    return run


def chart_service(workloads=[], overrides={}):
    """charts/<name> with its kind-profile values, running the released image (UIs)."""

    def run(c, ctx):
        service(
            c.name,
            NAMESPACE,
            TEMPLATE_VARS,
            overrides=overrides,
            workloads=workloads,
            resource_deps=ctx.deps,
            labels=[c.group],
        )

    return run


def remote_chart_service(repo, profile_values=False):
    """A third-party chart at the version third_party/<name> pins.

    repo:           oci:// or https:// chart repository
    profile_values: render with the kind-profile values of <name>
    """

    def run(c, ctx):
        blob = remote_chart(
            c.name,
            repo,
            third_party_version(c.name, "chart"),
            NAMESPACE,
            values=[profile_values_file(c.name, TEMPLATE_VARS)] if profile_values else [],
        )
        k8s_yaml(blob)
        k8s_resource(c.name, objects=select_objects(blob, lambda o: True), resource_deps=ctx.deps, labels=[c.group])

    return run


EXPORT_SECRET = os.path.join(TILT_DIR, "hack/export-secret.sh")


def export_secret_resource(name, secret, key, dest, resource_deps, group, namespace=NAMESPACE):
    """A resource writing one key of a cluster Secret to TILT_DIR/<dest>."""
    local_resource(
        name,
        labels=[group],
        resource_deps=resource_deps,
        allow_parallel=True,
        deps=[EXPORT_SECRET],
        env={"KUBE_CONTEXT": CONTEXT, "NAMESPACE": namespace},
        dir=TILT_DIR,
        cmd=[EXPORT_SECRET, secret, key, dest],
    )


def export_secret(secret, key, dest):
    """Runtime: one key of a Secret in the platform namespace, written to TILT_DIR/<dest>."""

    def run(c, ctx):
        export_secret_resource(c.name, secret, key, dest, ctx.deps, c.group)

    return run


# --- selection and deployment


def expand(names, bundles):
    """Component names for a list of bundle and component names, bundles nesting."""
    out = []
    todo = list(names)
    for _ in range(1000):
        if not todo:
            break
        name = todo.pop(0)
        if name in bundles:
            todo = list(bundles[name]) + todo
        elif name not in out:
            out.append(name)
    if todo:
        fail("bundles do not resolve, is one of them part of itself?")
    return out


def select(components, wanted, without):
    """Sorted names of wanted plus everything they need, minus without and whatever needs that."""
    for name in wanted + without:
        if name not in components:
            fail(
                "unknown bundle or component {}: bundles are in contrib/tilt/bundles.Tiltfile, components are {}".format(
                    name, ", ".join(sorted(components.keys()))
                )
            )

    blocked = {name: True for name in without}
    for _ in range(len(components)):
        for name, c in components.items():
            if name not in blocked and any([d in blocked for d in c.needs + c.alongside]):
                blocked[name] = True

    left_out = [name for name in wanted if name in blocked]
    if left_out:
        print("left out (--without {}): {}".format(",".join(without), " ".join(left_out)))

    selected = {}
    todo = list(wanted)
    edges = [d for c in components.values() for d in c.needs + c.alongside]
    for _ in range(len(todo) + len(edges) + 1):
        if not todo:
            break
        name = todo.pop()
        if name in selected or name in blocked:
            continue
        if name not in components:
            fail("unknown component {}".format(name))
        selected[name] = True
        todo.extend(components[name].needs + components[name].alongside)
    return sorted(selected.keys())


def _ctx(c, deployed, ready, selected, cache):
    def of(resources):
        def lookup(name):
            if name not in c.needs:
                fail("component {}: {} is not one of its needs".format(c.name, name))
            return resources[name]

        return lookup

    return struct(
        deps=[ready[n] for n in c.needs],
        ready=of(ready),
        deployed=of(deployed),
        selected=lambda name: name in selected,
        cache=cache,
    )


def deploy(components, selected):
    """Create the Tilt resources of the selected components, needs first."""
    deployed = {}
    ready = {}
    cache = {}
    for _ in range(len(selected)):
        for name in selected:
            c = components[name]
            if name in ready or any([n not in ready for n in c.needs]):
                continue
            ctx = _ctx(c, deployed, ready, selected, cache)
            last = (c.runtime(c, ctx) or c.name) if c.runtime else None
            if c.ready:
                cmds = [r(ctx) if type(r) == "function" else r for r in c.ready]
                gate(c.name + "-ready", cmds, [last] if last else ctx.deps, c.group)
            deployed[name] = last or c.name + "-ready"
            ready[name] = c.name + "-ready" if c.ready else last
    stuck = [name for name in selected if name not in ready]
    if stuck:
        fail("components need each other in a cycle: {}".format(", ".join(stuck)))
