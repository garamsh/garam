# The test image (ADR-0092): a file source another repository's CI
# copies three paths out of with `docker create` and `docker cp`, and
# never runs. README.md beside this file is its contract. Build context
# is the repository root; Dockerfile.dockerignore beside this file
# replaces the root's .dockerignore, which keeps platform/tests/ out
# of the release image.

# The base deploy/local/Dockerfile builds garam on, pinned by digest on
# the same terms; `make -C tooling check-toolchain` holds its tag
# against platform/go.mod.
FROM mirror.gcr.io/library/golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build

WORKDIR /src

# platform/go.mod replaces lib/errorx and lib/grn with their folders
# (ADR-0132), so those modules' manifests come along.
COPY platform/go.mod platform/go.sum ./platform/
COPY lib/errorx/go.mod lib/errorx/go.sum ./lib/errorx/
COPY lib/grn/go.mod lib/grn/go.sum ./lib/grn/
RUN go -C platform mod download

COPY . .
# CGO_ENABLED=0 is what makes both executables static: the consumer
# runs them on its own host, which shares no C library with this stage.
RUN CGO_ENABLED=0 go build -o /out/garam ./platform/cmd/garam \
  && CGO_ENABLED=0 go build -o /out/testprincipal ./platform/tests/testprincipal

FROM scratch

COPY --from=build /out/garam /garam
COPY --from=build /out/testprincipal /testprincipal
COPY platform/migrations /migrations

# The commit built, which the image's tag repeats. It is what the build
# claimed and not a property of the bytes, as on the release images.
# The source label names the repository the image is built from.
ARG IMAGE_REVISION
LABEL org.opencontainers.image.revision=$IMAGE_REVISION \
  org.opencontainers.image.source=https://github.com/garamsh/garam \
  org.opencontainers.image.description="Test image: /garam, /testprincipal and /migrations/ for docker cp; never run"

# `docker create` refuses an image with no command, so one is named;
# nothing runs it.
ENTRYPOINT ["/garam"]
