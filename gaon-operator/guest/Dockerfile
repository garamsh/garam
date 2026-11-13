# The composed guest image of a gaon MicroVM (ADR-0151 §3, its user by
# ADR-0153): one root that holds the agent runtime, the workspace and
# garam's adapter, and the supervisor that is its entrypoint. Build
# context is the repository root; `make -C gaon-operator guest-image`
# builds it.
#
# Each program's image is pinned by digest and the three digests are
# recorded as labels. The registry holds each as one manifest, not an
# index, so that manifest's digest is the image's. A move is a change
# to these lines and to nothing else.
ARG AGENT_IMAGE=486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/sherlock/agent:0.10.0@sha256:fe7e426bc2ad7c6d5c31c96daf699fb1b801a6c9c14c3d917e7297a0cbd264cf
ARG WORKSPACE_IMAGE=486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/sherlock/workspace:0.10.0@sha256:acb6679005542bfd01705ae6a12926dee9a3f7ed839a673b14ec78f29a1fd489
ARG ADAPTER_IMAGE=486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam/server:0.7.0@sha256:d0798604733e7a1b7becedfe841add01a7f6f8423e2a1b222e1d6eb20bff1997

# The builder is the one deploy/local/Dockerfile builds garam with.
FROM mirror.gcr.io/library/golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build

WORKDIR /src

# gaon-operator/go.mod replaces three lib modules with their folders, so
# their manifests come along for the download layer.
COPY gaon-operator/go.mod gaon-operator/go.sum ./gaon-operator/
COPY lib/errorx/go.mod lib/errorx/go.sum ./lib/errorx/
COPY lib/grn/go.mod lib/grn/go.sum ./lib/grn/
COPY lib/platform-sdk-go/go.mod lib/platform-sdk-go/go.sum ./lib/platform-sdk-go/
RUN for attempt in 1 2 3; do \
      go -C gaon-operator mod download && exit 0; \
      [ "$attempt" = 3 ] && exit 1; \
      echo "go mod download: attempt $attempt failed; retrying" >&2; \
      sleep $((attempt * 10)); \
    done

COPY gaon-operator ./gaon-operator
COPY lib/errorx ./lib/errorx
COPY lib/grn ./lib/grn
COPY lib/platform-sdk-go ./lib/platform-sdk-go
RUN CGO_ENABLED=0 go -C gaon-operator build -o /out/gaon-guest ./cmd/gaon-guest

FROM ${AGENT_IMAGE} AS agent
FROM ${ADAPTER_IMAGE} AS adapter

# The workspace image is the root: it holds the shell and coreutils an
# Exec runs in. The runtime's tools, its CA bundle and garam come in
# beside them.
FROM ${WORKSPACE_IMAGE}

ARG AGENT_IMAGE
ARG WORKSPACE_IMAGE
ARG ADAPTER_IMAGE
ARG REVISION=unknown
# The workspace image's own title, source and revision labels describe
# that image, so each is replaced.
LABEL org.opencontainers.image.title="gaon-guest" \
      org.opencontainers.image.source="https://github.com/garamsh/garam" \
      org.opencontainers.image.revision="${REVISION}" \
      sh.garam.guest.runtime-image="${AGENT_IMAGE}" \
      sh.garam.guest.workspace-image="${WORKSPACE_IMAGE}" \
      sh.garam.guest.adapter-image="${ADAPTER_IMAGE}"

COPY --from=agent /opt/sherlock/tools /opt/sherlock/tools
COPY --from=agent /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=adapter /usr/local/bin/garam /usr/local/bin/garam
COPY --from=build /out/gaon-guest /usr/local/bin/gaon-guest

# The runtime and the workspace are one binary, /sherlock, in both
# images. The build stops where the two releases differ, because the
# workspace image's copy is what runs as both programs.
RUN --mount=type=bind,from=agent,source=/sherlock,target=/mnt/agent-sherlock \
    cmp /sherlock /mnt/agent-sherlock

# The account the runtime and the adapter run as (ADR-0153), and the
# mount points the operator's MicroVM names. The directories stay
# root's: the supervisor gives the store and the outbox to 65532 at
# start, and the workspace manages /workspace as root. The image sets no
# USER: the supervisor is root and drops each program itself.
RUN groupadd --gid 65532 guest \
  && useradd --uid 65532 --gid 65532 --no-create-home --home-dir /nonexistent \
      --shell /usr/sbin/nologin guest \
  && mkdir -p /var/lib/garam/memory /workspace /run/garam

ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt \
    SHERLOCK_TOOLS_DIR=/opt/sherlock/tools

EXPOSE 8088
ENTRYPOINT ["/usr/local/bin/gaon-guest"]
CMD []
