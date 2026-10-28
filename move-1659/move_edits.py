# Read by move.py after the citation sweep; edit() records one change.

# --- Makefile -------------------------------------------------------------
M = "Makefile"
edit(M, "GO_DIRECTIVE := $(shell sed -n 's/^go //p' go.mod)",
     "GO_DIRECTIVE := $(shell sed -n 's/^go //p' platform/go.mod)")
edit(M, "# directive, which can be older than this module's and then cannot load\n# its packages. The floor is the version go.mod declares;",
     "# directive, which can be older than this module's and then cannot load\n# its packages. The floor is the version platform/go.mod declares;")
edit(M, "# Tool directives in go.mod: go.sum records each version and",
     "# Tool directives in platform/go.mod: its go.sum records each version and")

# buf reads buf.yaml from the directory it runs in.
edit(M, "PROTO_FILES = $(shell $(BUF) ls-files)", "PROTO_FILES = $(shell cd platform && $(BUF) ls-files)")
edit(M, "\t@$(BUF) lint\n", "\t@cd platform && $(BUF) lint\n")
edit(M, "# roots: `scripts/` and the repository root.", "# roots: `tooling/` and the repository root.")
edit(M, "$(shell find scripts -type f -name '*.sh')", "$(shell find tooling -type f -name '*.sh')")
edit(M, "\t@scripts/image-build-test.sh\n\t@scripts/image-test-ecr-test.sh\n",
     "\t@tooling/image-build-test.sh\n\t@tooling/image-test-ecr-test.sh\n")

# gen: every platform generator reads its config, and writes its output,
# relative to the module root.
edit(M, """\t$(OAPI_CODEGEN) --config platform/api/oapi-codegen.yaml platform/api/openapi.yaml
\t$(OAPI_CODEGEN) --config platform/api/client-codegen.yaml platform/api/openapi.yaml
\tgo run ./platform/internal/cli/cligen
\t$(OAPI_CODEGEN) --config platform/api/machine-codegen.yaml platform/api/machine.yaml
\t$(SQLC) generate
\t$(MOCKERY)
\t$(BUF) generate
\t$(GOIMPORTS) -w platform/proto/gen
\tcd cowork && $(OAPI_CODEGEN) --config platform/api/oapi-codegen.yaml platform/api/openapi.yaml
""", """\tcd platform && $(OAPI_CODEGEN) --config api/oapi-codegen.yaml api/openapi.yaml
\tcd platform && $(OAPI_CODEGEN) --config api/client-codegen.yaml api/openapi.yaml
\tcd platform && go run ./internal/cli/cligen
\tcd platform && $(OAPI_CODEGEN) --config api/machine-codegen.yaml api/machine.yaml
\tcd platform && $(SQLC) generate
\tcd platform && $(MOCKERY)
\tcd platform && $(BUF) generate
\tcd platform && $(GOIMPORTS) -w proto/gen
\tcd cowork && $(OAPI_CODEGEN) --config api/oapi-codegen.yaml api/openapi.yaml
""")
edit(M, """GEN_PATHS := $(shell { sed -n 's/^output: //p' platform/api/oapi-codegen.yaml platform/api/client-codegen.yaml \\
\t\tplatform/api/machine-codegen.yaml; \\
\tsed -n 's/^        out: //p' sqlc.yaml; \\
\tsed -n 's/^    out: //p' buf.gen.yaml; \\
\tsed -n 's|^  $(shell sed -n 's/^module //p' go.mod)/\\(.*\\):$$|\\1/mocks|p' .mockery.yaml; \\
\tsed -n 's/^\\toutPath *= "\\(.*\\)"$$/\\1/p' platform/internal/cli/cligen/main.go; \\
""", """GEN_PATHS := $(shell { sed -n 's|^output: |platform/|p' platform/api/oapi-codegen.yaml platform/api/client-codegen.yaml \\
\t\tplatform/api/machine-codegen.yaml; \\
\tsed -n 's|^        out: |platform/|p' platform/sqlc.yaml; \\
\tsed -n 's|^    out: |platform/|p' platform/buf.gen.yaml; \\
\tsed -n 's|^  $(shell sed -n 's/^module //p' platform/go.mod)/\\(.*\\):$$|platform/\\1/mocks|p' platform/.mockery.yaml; \\
\tsed -n 's|^\\toutPath *= "\\(.*\\)"$$|platform/\\1|p' platform/internal/cli/cligen/main.go; \\
""")
edit(M, "\tgo.mod,$(GO_DIRECTIVE),deploy/local/Dockerfile,golang \\\n\tgo.mod,$(GO_DIRECTIVE),platform/tests/testprincipal/Dockerfile,golang",
     "\tplatform/go.mod,$(GO_DIRECTIVE),deploy/local/Dockerfile,golang \\\n\tplatform/go.mod,$(GO_DIRECTIVE),platform/tests/testprincipal/Dockerfile,golang")
edit(M, "\t@scripts/check-toolchain.sh", "\t@tooling/check-toolchain.sh")
edit(M, "\t@scripts/search.sh", "\t@tooling/search.sh")
edit(M, "\t@scripts/db-prune.sh", "\t@tooling/db-prune.sh")

# mutation: gremlins mutates the module it runs in.
edit(M, "# mutation runs gremlins over internal/ and reports",
     "# mutation runs gremlins over platform/internal/ and reports")
edit(M, "\t@echo 'mutation: internal/, coefficient", "\t@echo 'mutation: platform/internal/, coefficient")
edit(M, "\t@$(GREMLINS) unleash --timeout-coefficient $(MUTATION_COEFFICIENT) -o '$(MUTATION_REPORT)' internal",
     "\t@cd platform && $(GREMLINS) unleash --timeout-coefficient $(MUTATION_COEFFICIENT) -o '$(CURDIR)/$(MUTATION_REPORT)' internal")
edit(M, "# at a disk: each worker copies the module, console/ builds included.",
     "# at a disk: each worker copies the module.")

edit(M, "# web/ — the Next.js frontend (ADR-0009).", "# console/ — the Next.js frontend (ADR-0009).")
edit(M, "# The candidates are what git has under web/ —", "# The candidates are what git has under console/ —")
edit(M, "nothing to check — git lists no file under web/'", "nothing to check — git lists no file under console/'")
edit(M, "# The script runs in web/, so the path it names is relative to it.",
     "# The script runs in console/, so the path it names is relative to it.")
edit(M, "-o \\([^\" ]*\\)\".*|web/\\1|p' console/package.json)", "-o \\([^\" ]*\\)\".*|console/\\1|p' console/package.json)")

# The chart's sweep of what the Go source reads.
edit(M, "--include=*.go internal cmd |", "--include=*.go platform/internal platform/cmd |")
edit(M, "--exclude=*_test.go internal cmd | wc -l", "--exclude=*_test.go platform/internal platform/cmd | wc -l")
edit(M, "reads of the environment under internal|cmd,", "reads of the environment under platform/internal|cmd,")
edit(M, "--exclude=*_test.go internal cmd || true", "--exclude=*_test.go platform/internal platform/cmd || true")

edit(M, "\t\tdocker build -t garam-web:local -f deploy/local/Dockerfile.web web",
     "\t\tdocker build -t garam-web:local -f deploy/local/Dockerfile.web console")

# release.yml names each image by its build context, which --names
# prints and the Makefile and image-build-test.sh read.
edit(M, "| sed -n 's|^web ||p')", "| sed -n 's|^console ||p')")
edit(".github/workflows/release.yml", "          context: web\n", "          context: console\n")
T = "tooling/image-build-test.sh"
edit(T, "mkdir -p \"$repo/scripts\" \"$repo/.github/workflows\"", "mkdir -p \"$repo/tooling\" \"$repo/.github/workflows\"")
edit(T, "cp tooling/image-build.sh \"$repo/scripts/\"", "cp tooling/image-build.sh \"$repo/tooling/\"")
edit(T, "accepted 'web garam-web:ci' --names", "accepted 'console garam-web:ci' --names")
edit(T, "accepted \"web $reg/$ns/garam-web:$short\" --names", "accepted \"console $reg/$ns/garam-web:$short\" --names")
E = "tooling/image-test-ecr-test.sh"
edit(E, "mkdir -p \"$repo/scripts\" \"$repo/tests/image\"", "mkdir -p \"$repo/tooling\" \"$repo/platform/tests/image\"")
edit(E, "cp tooling/image-test-push.sh tooling/image-test-pin.sh \"$repo/scripts/\"",
     "cp tooling/image-test-push.sh tooling/image-test-pin.sh \"$repo/tooling/\"")
edit(E, "cat >\"$repo/scripts/image-test.sh\"", "cat >\"$repo/tooling/image-test.sh\"")
edit(E, "cat >\"$repo/tests/image/test-image.sh\"", "cat >\"$repo/platform/tests/image/test-image.sh\"")
edit(E, "chmod +x \"$repo/scripts/image-test.sh\" \"$repo/tests/image/test-image.sh\"",
     "chmod +x \"$repo/tooling/image-test.sh\" \"$repo/platform/tests/image/test-image.sh\"")
edit(E, "echo changed >>\"$repo/scripts/image-test.sh\"", "echo changed >>\"$repo/tooling/image-test.sh\"")
edit(E, "read against $root/scripts/image-test-push.sh and $root/scripts/image-test-pin.sh",
     "read against $root/tooling/image-test-push.sh and $root/tooling/image-test-pin.sh")

# The workflows: the Go toolchain is the one the platform's go.mod names.
for wf, n in ((".github/workflows/ci.yml", 3),):
    edit(wf, "go-version-file: go.mod", "go-version-file: platform/go.mod", n)
edit(".github/workflows/images.yml", "and the web image's context is `web/`.", "and the web image's context is `console/`.")

# Dependabot reads each module where its manifest now is.
D = ".github/dependabot.yml"
edit(D, "# Three record it here instead: go.mod's `go` directive,", "# Three record it here instead: platform/go.mod's `go` directive,")
edit(D, "No entry here reaches web/ at\n# all,", "No entry here reaches console/ at\n# all,")
edit(D, "    directory: \"/tests/testprincipal\"", "    directory: \"/platform/tests/testprincipal\"")
edit(D, "  # go.mod's requirements, the seven generator pins,", "  # platform/go.mod's requirements, the seven generator pins,")
edit(D, """  - package-ecosystem: "gomod"
    directory: "/"
""", """  - package-ecosystem: "gomod"
    directory: "/platform"
""")
edit(D, "library. The root module replaces platform/sdk", "library. The platform module replaces platform/sdk")

# The images: each Go build reads the platform module's manifests.
for df in ("deploy/local/Dockerfile", "platform/tests/testprincipal/Dockerfile"):
    edit(df, "COPY go.mod go.sum ./\n", "COPY platform/go.mod platform/go.sum ./platform/\n")
edit("deploy/local/Dockerfile", "      go mod download && exit 0; \\", "      go -C platform mod download && exit 0; \\")
edit("deploy/local/Dockerfile", "COPY migrations /app/migrations", "COPY platform/migrations /app/migrations")
edit("deploy/local/Dockerfile", "go.mod replaces", "platform/go.mod replaces")
edit("deploy/local/Dockerfile.web", "Build context is `web/`.", "Build context is `console/`.")
P = "platform/tests/testprincipal/Dockerfile"
edit(P, "RUN go mod download\n", "RUN go -C platform mod download\n")
edit(P, "go build -o /out/garam ./cmd/garam \\\n  && CGO_ENABLED=0 go build -o /out/testprincipal ./tests/testprincipal",
     "go build -o /out/garam ./platform/cmd/garam \\\n  && CGO_ENABLED=0 go build -o /out/testprincipal ./platform/tests/testprincipal")
edit(P, "COPY migrations /migrations", "COPY platform/migrations /migrations")
edit(P, "which keeps tests/ out of the\n# release image.", "which keeps platform/tests/ out\n# of the release image.")
edit(P, "holds its tag against go.mod.", "holds its tag against platform/go.mod.")
edit(P, "# go.mod replaces platform/sdk with its folder", "# platform/go.mod replaces platform/sdk with its folder")
I = "platform/tests/testprincipal/Dockerfile.dockerignore"
edit(I, "# Build context for tests/testprincipal/Dockerfile, read in place of the\n# root .dockerignore. The same entries, less `tests`: the build compiles\n# tests/testprincipal.",
     "# Build context for platform/tests/testprincipal/Dockerfile, read in\n# place of the root .dockerignore. The same entries, less\n# `platform/tests`: the build compiles platform/tests/testprincipal.")
edit(I, "\nweb\n", "\nconsole\n")
edit(".dockerignore", "\nweb\n", "\nconsole\n")
edit(".dockerignore", "\ntests\n", "\nplatform/tests\n")
edit("deploy/local/docker-compose.yml", "      context: ../../web\n", "      context: ../../console\n")

# cligen writes its output relative to the directory it runs in.
edit("README.md", "`platform/api/openapi.yaml` by `go run ./platform/internal/cli/cligen`,",
     "`platform/api/openapi.yaml` by `go run ./internal/cli/cligen` in `platform/`,")

# The TypeScript tests that read files across the tree, from the root.
edit("console/src/app/(marketing)/__tests__/claims.test.ts", 'const WEB_ROOT = path.join(REPO_ROOT, "web");',
     'const WEB_ROOT = path.join(REPO_ROOT, "console");')
edit("console/src/lib/__tests__/return-path.test.ts", '"../../../../internal/api/testdata/return-path-vectors.json"',
     '"../../../../platform/internal/api/testdata/return-path-vectors.json"')
C = "console/src/app/(marketing)/__tests__/claims.test.ts"
edit(C, 'path.join(REPO_ROOT, "migrations")', 'path.join(REPO_ROOT, "platform/migrations")')
edit(C, 'path.join(REPO_ROOT, "migrations", file)', 'path.join(REPO_ROOT, "platform/migrations", file)')
# A claim that a path is gone names where it would now be.
edit(C, 'path: "internal/adapter", exists: false', 'path: "platform/internal/adapter", exists: false', 2)

# gen:api runs in console/, so the spec it reads is one level up and then into platform/.
edit("console/package.json", '"openapi-typescript ../api/openapi.yaml', '"openapi-typescript ../platform/api/openapi.yaml')

# go list patterns are relative to the directory make runs in, the root.
for suite in ("e2e", "workload", "standalone"):
    edit(M, f" ./tests/{suite}/...)", f" ./platform/tests/{suite}/...)", 2)
