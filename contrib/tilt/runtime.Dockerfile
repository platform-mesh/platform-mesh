# Thin runtime image for hot-reloaded components. The host builds the linux
# binary (see component_binary in helpers.py); Tilt live_update-syncs it into
# this image, so a code change never triggers a full docker build.
#
# NOT distroless, deliberately. Tilt implements live_update by exec'ing `tar`
# inside the running container, and the restart_process extension additionally
# needs `sh`, `touch`, `chmod` and `date` to arm its restart trigger. A
# distroless/scratch base has none of them, and every sync dies with
#
#   OCI runtime exec failed: exec: "tar": executable file not found in $PATH
#
# (https://github.com/tilt-dev/tilt/issues/4303). busybox-via-alpine supplies
# the lot for ~8MB. This image only ever runs in the kind dev cluster; the
# images a real install pulls are built by each component's own Dockerfile and
# stay distroless.
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

# The build context is ./bin/<component>: the binary as `entrypoint` plus any
# payload directories the component reads at runtime (component_binary's
# extra_dirs). It lands in a directory owned by the runtime user, NOT at `/`:
# the live_update sync runs as the container's user, and replacing a file
# means writing to its parent directory. Rooted at `/` that needs root, which
# would mean giving up the nonroot parity the production image has.
COPY . /app/
RUN chown -R 65532:65532 /app

# Fixed path + fixed entrypoint. The production charts set container `args` only
# and rely on the image's entrypoint being the operator, so a per-component path
# would never be invoked. live_update syncs the rebuilt binary to this same
# path. The ENTRYPOINT here is a fallback for a plain `docker run` — under Tilt
# the restart_process extension replaces it with its wrapper around this path
# (see component_binary in helpers.py), preserving args-append semantics.
USER 65532:65532
ENTRYPOINT ["/app/entrypoint"]
