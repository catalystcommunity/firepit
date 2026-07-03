# firepit-webapp container image (task D1, PLANDOC.md §3/§6).
#
# Multi-stage: Vite build produces the static SPA bundle, then an
# unprivileged nginx serves it. Build from the REPO ROOT so `clients/`
# (the generated TS client webapp/tsconfig.app.json path-aliases against;
# see webapp/src/gen and PLANDOC.md §6) is in the build context alongside
# webapp/ and version/ (whose VERSION.txt some webapp code may read at
# build time, mirroring longhouse's Dockerfile.web convention):
#
#   docker build -f webapp/Dockerfile -t firepit/webapp:dev .
#
# tools.sh's build-images verb and docker-compose.yaml's (future) webapp
# service both build it this way.
FROM node:22-alpine AS builder
WORKDIR /build

COPY webapp/package.json webapp/package-lock.json* ./webapp/
RUN cd webapp && npm ci

COPY version/ ./version/
COPY clients/ ./clients/
COPY webapp/ ./webapp/
RUN cd webapp && npm run build

# nginx-unprivileged: same nginx binary and stock docker-entrypoint.d
# scripts as the official nginx image (including the envsubst-on-templates
# feature nginx.conf relies on for ${FIREPIT_API_URL}), but pre-configured
# to run as an arbitrary non-root UID out of the box (writable
# /var/cache/nginx, /etc/nginx/conf.d, etc., and a non-privileged default
# listen port) — no manual chown/setcap dance needed to satisfy
# runAsNonRoot in helm_chart's podSecurityContext.
FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY --from=builder /build/webapp/dist /usr/share/nginx/html
COPY webapp/nginx.conf /etc/nginx/templates/default.conf.template

# Turns on the base image's built-in 15-local-resolvers.envsh, which reads
# the container's own /etc/resolv.conf nameservers into
# $NGINX_LOCAL_RESOLVERS — consumed by nginx.conf's `resolver` directive so
# proxying to the api Service defers DNS resolution to request time instead
# of failing nginx's startup if that Service isn't resolvable yet.
ENV NGINX_ENTRYPOINT_LOCAL_RESOLVERS=1

EXPOSE 8080
