# Thin runtime image for hot-reloaded components. The host builds the linux
# binary (see component_build in helpers.py); Tilt live_update-syncs it into
# this image, so a code change never triggers a full docker build.
FROM gcr.io/distroless/static:nonroot
ARG BIN
COPY ${BIN} /${BIN}
# component_build passes the same BIN; the chart's command/args select it.
