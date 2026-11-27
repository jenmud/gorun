# syntax=docker/dockerfile:1

# Two images, configured with environment variables (see docs/web.md#running-in-docker):
#
#   docker build -t gorun .                    the web service on Alpine (the default, last stage)
#   docker build --target llm -t gorun:llm .   the same on Debian, with the assistant's model built
#                                              in (about 3 GB), so its first question needn't
#                                              download anything; `make docker` builds it

# --- with the model built in --------------------------------------------------------------
# llama.cpp's Linux build needs glibc, so this image is Debian, and gorun is built for glibc too.

FROM golang:1.27-bookworm AS build-glibc
ARG VERSION=devel
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata \
      -ldflags "-s -w -X main.version=${VERSION}" -o /out/gorun ./cmd

# gorun downloads llama.cpp and the model itself, checked against the digests it pins. The cache
# mount keeps them between builds, and only the ones this gorun uses are copied into the image.
FROM build-glibc AS model
RUN --mount=type=cache,target=/cache \
    GORUN_LLM_DIR=/cache /out/gorun -pull-model > /tmp/pulled \
 && mkdir -p /opt/gorun/llm/models \
 && cp -a "$(awk '$1 == "llama.cpp" { print $2 }' /tmp/pulled)" /opt/gorun/llm/ \
 && cp -a "$(awk '$1 == "model" { print $2 }' /tmp/pulled)" /opt/gorun/llm/models/

FROM debian:bookworm-slim AS llm
# libffi8, libgomp1, libstdc++6: what gorun's llama.cpp bindings and llama.cpp load; wget: the
# health check. The model runs on the CPU: llama.cpp falls back to it without a GPU driver.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates libffi8 libgomp1 libstdc++6 wget \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --system gorun \
 && useradd --system --gid gorun --home-dir /var/lib/gorun --shell /usr/sbin/nologin gorun \
 && mkdir -p /etc/gorun /var/lib/gorun \
 && chown gorun:gorun /var/lib/gorun
COPY --from=build-glibc /out/gorun /usr/local/bin/gorun
COPY --chmod=755 docker/entrypoint.sh /usr/local/bin/entrypoint.sh
# outside /var/lib/gorun, so a volume mounted there doesn't hide it
COPY --from=model /opt/gorun/llm /opt/gorun/llm

ENV GORUN_ADDRESS=:8080 \
    GORUN_DB_PATH=/var/lib/gorun/gorun.db \
    GORUN_LOG_FILE=/var/lib/gorun/gorun.log \
    GORUN_CACHE_DIR=/var/lib/gorun/cache \
    GORUN_LLM_DIR=/opt/gorun/llm \
    HOME=/var/lib/gorun

USER gorun
WORKDIR /var/lib/gorun
VOLUME /var/lib/gorun
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD sh -c 'wget -q -O /dev/null "http://127.0.0.1:${GORUN_ADDRESS##*:}/"' || exit 1

ENTRYPOINT ["entrypoint.sh"]

# --- the default: Alpine, without the model --------------------------------------------------
# The assistant still works here when GORUN_LLM_URL points at a model server; the built-in model
# needs the llm image, since llama.cpp's Linux build doesn't run on Alpine.

FROM golang:1.27-alpine AS build
ARG VERSION=devel
WORKDIR /src
COPY . .
# timetzdata embeds the time zone database, so the image needs no tzdata package.
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata \
      -ldflags "-s -w -X main.version=${VERSION}" -o /out/gorun ./cmd

FROM alpine:3.21
# libffi: gorun's llama.cpp bindings load it when the built-in model is first used
RUN apk add --no-cache ca-certificates libffi \
 && addgroup -S gorun \
 && adduser -S -G gorun -h /var/lib/gorun gorun \
 && mkdir -p /etc/gorun /var/lib/gorun \
 && chown gorun:gorun /var/lib/gorun
COPY --from=build /out/gorun /usr/local/bin/gorun
COPY --chmod=755 docker/entrypoint.sh /usr/local/bin/entrypoint.sh

# Each variable can be overridden with docker run -e. GORUN_FILE names an optional Gorunfile.
ENV GORUN_ADDRESS=:8080 \
    GORUN_DB_PATH=/var/lib/gorun/gorun.db \
    GORUN_LOG_FILE=/var/lib/gorun/gorun.log \
    GORUN_CACHE_DIR=/var/lib/gorun/cache \
    HOME=/var/lib/gorun

USER gorun
WORKDIR /var/lib/gorun
VOLUME /var/lib/gorun
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD sh -c 'wget -q -O /dev/null "http://127.0.0.1:${GORUN_ADDRESS##*:}/"' || exit 1

ENTRYPOINT ["entrypoint.sh"]
