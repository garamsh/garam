# Multi-stage build of the single garam binary (ADR-0003). The same
# image runs `migrate`, `serve authz` and `serve api`; the compose file
# picks the subcommand, and its healthchecks run `probe` — the runtime
# stage is scratch, so no shell or wget is there to ask instead. Build
# context is the repository root.

# The base is pinned by digest: the tag reads and the digest resolves,
# so two builds of this commit pull the same bytes. Dependabot proposes
# a newer tag — `.github/dependabot.yml` names what it does not cover —
# and a move by hand re-resolves with
# `docker buildx imagetools inspect <ref>`. It is pulled from Google's
# mirror of Docker's official image, the same digest: Docker Hub
# refuses anonymous pulls past a per-IP limit a CI runner shares with
# whoever held its address before (#1337), and ECR Public caps
# anonymous data across the addresses hosted runners share (#1532).
FROM mirror.gcr.io/library/golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build

WORKDIR /src

# Dependencies resolve from the manifests alone, so this layer survives
# source-only edits. The proxy has dropped a read mid-download (#1622),
# so the download is tried three times, 10 and 20 seconds apart. Each
# attempt fetches only what the last left missing, and verifies every
# module against go.sum as the first did. platform/go.mod replaces
# platform/sdk with its folder (ADR-0125), so that module's manifests
# come along.
COPY platform/go.mod platform/go.sum ./platform/
COPY platform/sdk/go.mod platform/sdk/go.sum ./platform/sdk/
RUN for attempt in 1 2 3; do \
      go -C platform mod download && exit 0; \
      [ "$attempt" = 3 ] && exit 1; \
      echo "go mod download: attempt $attempt failed; retrying" >&2; \
      sleep $((attempt * 10)); \
    done

COPY . .
RUN CGO_ENABLED=0 go build -o /out/garam ./platform/cmd/garam

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
COPY platform/migrations /app/migrations

# The commit and the version the build was given, recorded on the image
# so a deployment resolves back to a source without going through the
# git tag, which moves. Both are strings the builder passes in and
# neither is derived from the layers, so each records what the build
# claimed rather than what the image is. Unset, both labels are present
# and empty, so a build that claimed nothing reads differently from an
# image built before they existed.
ARG IMAGE_REVISION
ARG IMAGE_VERSION
LABEL org.opencontainers.image.revision=$IMAGE_REVISION \
  org.opencontainers.image.version=$IMAGE_VERSION

WORKDIR /app
USER garam
ENTRYPOINT ["garam"]
