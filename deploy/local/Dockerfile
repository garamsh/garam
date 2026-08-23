# Multi-stage build of the single garam binary (ADR-0003). The same
# image runs `migrate`, `serve authz` and `serve api`; the compose file
# picks the subcommand, and its healthchecks run `probe` — the runtime
# stage is scratch, so no shell or wget is there to ask instead. Build
# context is the repository root.

# The base is pinned by digest: the tag reads and the digest resolves,
# so two builds of this commit pull the same bytes. Dependabot proposes
# a newer tag — `.github/dependabot.yml` names what it does not cover —
# and a move by hand re-resolves with
# `docker buildx imagetools inspect <ref>`.
FROM golang:1.27.0-alpine3.24@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build

WORKDIR /src

# Dependencies resolve from the manifests alone, so this layer survives
# source-only edits.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/garam ./cmd/garam

# The account the processes run as, created here because scratch has no
# adduser. Only the garam lines travel: the image carries no root
# account, and the sealer's ownership check reads the numeric UID,
# which stays 10001.
RUN adduser -D -u 10001 garam \
  && grep '^garam:' /etc/passwd > /out/passwd \
  && grep '^garam:' /etc/group > /out/group

FROM scratch

# ca-certificates: `serve api` performs Google OIDC discovery over HTTPS.
# The module never names a time zone (no time.LoadLocation, no
# time.Local), so the image carries no zoneinfo.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/passwd /etc/passwd
COPY --from=build /out/group /etc/group

COPY --from=build /out/garam /usr/local/bin/garam
COPY migrations /app/migrations

WORKDIR /app
USER garam
ENTRYPOINT ["garam"]
