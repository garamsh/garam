# Multi-stage build of the single garam binary (ADR-0003). The same
# image runs `migrate`, `serve authz` and `serve api`; the compose file
# picks the subcommand. Build context is the repository root.

FROM golang:1.26.5-alpine3.24 AS build

WORKDIR /src

# Dependencies resolve from the manifests alone, so this layer survives
# source-only edits.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/garam ./cmd/garam

FROM alpine:3.24.1

# ca-certificates: `serve api` performs Google OIDC discovery over HTTPS.
RUN apk add --no-cache ca-certificates \
  && adduser -D -u 10001 garam

COPY --from=build /out/garam /usr/local/bin/garam
COPY migrations /app/migrations

WORKDIR /app
USER garam
ENTRYPOINT ["garam"]
