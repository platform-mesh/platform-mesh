# Thin runtime image for hot-reloaded components. The host builds the linux
# binary (see component_build in helpers.py); Tilt live_update-syncs it into
# this image, so a code change never triggers a full docker build.
FROM gcr.io/distroless/static:nonroot
ARG BIN
# Land the binary at a fixed path with a fixed entrypoint. The production charts
# set container args only and rely on the image's entrypoint to be the operator,
# so a per-component path (ARG can't be used in an exec-form ENTRYPOINT) wouldn't
# be invoked. live_update syncs the rebuilt binary to the same /entrypoint.
COPY ${BIN} /entrypoint
ENTRYPOINT ["/entrypoint"]
