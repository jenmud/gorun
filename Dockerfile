# syntax=docker/dockerfile:1

# Builds gorun as a static binary, then runs it on Alpine, which provides the /bin/sh that
# local tasks need. Configure it with environment variables; see docs/web.md#running-in-docker.
FROM golang:1.26-alpine AS build
ARG VERSION=devel
WORKDIR /src
COPY . .
# timetzdata embeds the time zone database, so the image needs no tzdata package.
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata \
      -ldflags "-s -w -X main.version=${VERSION}" -o /out/gorun ./cmd

FROM alpine:3.21
RUN apk add --no-cache ca-certificates \
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
