# The test image (ADR-0092): a file source another repository's CI
# copies three paths out of with `docker create` and `docker cp`, and
# never runs. README.md beside this file is its contract. Build context
# is the repository root; Dockerfile.dockerignore beside this file
# replaces the root's .dockerignore, which keeps tests/ out of the
# release image.

# The base deploy/local/Dockerfile builds garam on, pinned by digest on
# the same terms; `make check-toolchain` holds its tag against go.mod.
FROM golang:1.27.1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 is what makes both executables static: the consumer
# runs them on its own host, which shares no C library with this stage.
RUN CGO_ENABLED=0 go build -o /out/garam ./cmd/garam \
  && CGO_ENABLED=0 go build -o /out/testprincipal ./tests/testprincipal

FROM scratch

COPY --from=build /out/garam /garam
COPY --from=build /out/testprincipal /testprincipal
COPY migrations /migrations

# The commit built, which the image's tag repeats. It is what the build
# claimed and not a property of the bytes, as on the release images.
# The source label is what links a GHCR package to this repository.
ARG IMAGE_REVISION
LABEL org.opencontainers.image.revision=$IMAGE_REVISION \
  org.opencontainers.image.source=https://github.com/garamsh/garam \
  org.opencontainers.image.description="Test image: /garam, /testprincipal and /migrations/ for docker cp; never run"

# `docker create` refuses an image with no command, so one is named;
# nothing runs it.
ENTRYPOINT ["/garam"]
