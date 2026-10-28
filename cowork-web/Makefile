# cowork-web — cowork's Next.js app (ADR-0127). It installs from the
# workspace's lockfile at the root, as console does. Its verbs are
# every module's (#1661); `make -C cowork-web ci` is its fast gate, and
# `stack-up suites` the half that needs Docker.

include ../tooling/make/stack.mk

.PHONY: ci install format lint test build gen check-gen stack-up suites e2e

ci: install format lint test build check-gen

install:
	pnpm install --frozen-lockfile

FILES = $(shell git ls-files --cached --others --exclude-standard)

lint:
	@echo 'lint: $(words $(filter %.ts %.tsx %.mjs %.mts,$(FILES))) files under cowork-web (eslint, tsc)'
	@test -n '$(filter %.ts %.tsx,$(FILES))' || { echo 'lint: nothing to lint — git lists no TypeScript under cowork-web'; exit 1; }
	pnpm lint
	pnpm typecheck

format:
	@echo 'format: $(words $(FILES)) files under cowork-web (prettier, less its ignore files)'
	@test -n '$(FILES)' || { echo 'format: nothing to check — git lists no file under cowork-web'; exit 1; }
	pnpm format

test:
	pnpm test

# `next build` runs with NODE_ENV=production, where env.ts requires
# every URL. Nothing calls them during the build, so placeholders do.
build:
	COWORK_API_URL=http://localhost:8090 COWORK_SYNC_URL=ws://localhost:8090 \
		GARAM_API_URL=http://localhost:8080 GARAM_CONSOLE_URL=http://localhost:3000 pnpm build

gen:
	pnpm gen:api

GEN_PATHS = $(shell sed -n 's|.*"gen:api": ".* -o \([^" ]*\)".*|\1|p' package.json)

check-gen: gen
	@echo 'check-gen: $(words $(GEN_PATHS)) generated paths compared'
	@test -n '$(GEN_PATHS)' && git ls-files --error-unmatch -- $(GEN_PATHS) >/dev/null 2>&1 \
		|| { echo 'check-gen: nothing to compare — package.json names no gen:api output, or git tracks none of "$(GEN_PATHS)"'; exit 1; }
	@git diff --exit-code -- $(GEN_PATHS)

# The platform the browser suite signs in through, which the console's
# suite drives too.
stack-up:
	@$(MAKE) --no-print-directory -C ../deploy/local backend-up

suites: install e2e

# e2e drives the built app and the built cowork binary in a real
# browser, against the platform stack-up starts. cowork keeps its own
# database on the stack's Postgres, created here where absent; the seed
# writes sessions into the platform's.
E2E_DB_NAME = cowork_e2e
E2E_DATABASE_URL = $(if $(DB_HOST_ADDR),postgres://garam:garam@$(DB_HOST_ADDR)/$(E2E_DB_NAME)?sslmode=disable)
COWORK_DIR := $(abspath ..)

e2e:
	@test -n "$(API_HOST_ADDR)" || { echo "e2e: no api published on this host — run make -C cowork-web stack-up" >&2; exit 1; }
	@test -n '$(E2E_DATABASE_URL)' || { echo "e2e: no postgres published on this host — run make -C cowork-web stack-up" >&2; exit 1; }
	@present="$$($(COMPOSE_LOCAL) exec -T postgres psql -U garam -d postgres -tAc \
		"SELECT 1 FROM pg_database WHERE datname='$(E2E_DB_NAME)'")" \
		|| { echo "e2e: could not ask postgres whether $(E2E_DB_NAME) exists" >&2; exit 1; }; \
		test -n "$$present" || $(COMPOSE_LOCAL) exec -T postgres createdb -U garam $(E2E_DB_NAME)
	cd $(COWORK_DIR) && go build -o bin/cowork ./cmd/cowork
	pnpm exec playwright install chromium
	COWORK_BIN='$(COWORK_DIR)/bin/cowork' COWORK_MIGRATIONS_DIR='$(COWORK_DIR)/migrations' \
		COWORK_E2E_DATABASE_URL='$(E2E_DATABASE_URL)' GARAM_API_URL=http://$(API_HOST_ADDR) \
		GARAM_E2E_DATABASE_URL='$(WEB_E2E_DATABASE_URL)' pnpm e2e
