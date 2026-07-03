# firepit-api container image (task B1/D1, PLANDOC.md §3, §6).
#
# Build from the REPO ROOT (not api/), since api/go.mod has:
#   replace github.com/catalystcommunity/firepit/coredb => ../coredb
# and needs that sibling module's source alongside it:
#
#   docker build -f api/Dockerfile -t firepit/api:dev .
#
# docker-compose.yaml's `api` service sets context/dockerfile accordingly.
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /build/coredb
COPY coredb/go.mod coredb/go.sum* ./
COPY coredb/ ./

WORKDIR /build/api
COPY api/go.mod api/go.sum ./
RUN go mod download
COPY api/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /firepit-api ./cmd/firepit-api

# distroless/static: no shell, no package manager, just the binary + CA certs
# + the /etc/passwd entry for the nonroot UID below — smaller attack surface
# than alpine at runtime while still giving gorm/pgx TLS a valid cert bundle.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /firepit-api /firepit-api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/firepit-api"]
