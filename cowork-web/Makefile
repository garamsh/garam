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
		GARAM_API_URL=http://localhost:8080 GARAM_SITE_URL=http://localhost:3001 pnpm build

gen:
	pnpm gen:api

GEN_PATHS = $(shell sed -n 's|.*"gen:api": ".* -o \([^" ]*\)".*|\1|p' package.json)

check-gen: gen
	@echo 'check-gen: $(words $(GEN_PATHS)) generated paths compared'
	@test -n '$(GEN_PATHS)' && git ls-files --error-unmatch -- $(GEN_PATHS) >/dev/null 2>&1 \
		|| { echo 'check-gen: nothing to compare — package.json names no gen:api output, or git tracks none of "$(GEN_PATHS)"'; exit 1; }
	@git diff --exit-code -- $(GEN_PATHS)

# The platform the browser suite signs in through, which the console's
# suite drives too, and the stack's own cowork beside it, asking the
# policy service. The specs name cowork's API on localhost:8091 and its
# user-content origin on localhost:8093 (e2e/), so the stack publishes
# them there.
COWORK_API_PORT = 8091
COWORK_USERCONTENT_PORT = 8093

stack-up:
	@$(MAKE) --no-print-directory -C ../deploy/local backend-up
	@COWORK_LOCAL_API_PORT=$(COWORK_API_PORT) COWORK_LOCAL_USERCONTENT_PORT=$(COWORK_USERCONTENT_PORT) \
		$(MAKE) --no-print-directory -C ../deploy/local cowork-up

suites: install e2e

# e2e drives the built app and the site its sign-in goes through in a
# real browser, against the platform and the cowork stack-up starts. The
# seed writes users, organizations and sessions into the platform's
# database and returns once the policy service holds them.
e2e:
	@test -n "$(API_HOST_ADDR)" || { echo "e2e: no api published on this host — run make -C cowork-web stack-up" >&2; exit 1; }
	@test -n '$(WEB_E2E_DATABASE_URL)' || { echo "e2e: no postgres published on this host — run make -C cowork-web stack-up" >&2; exit 1; }
	@test "$$($(COMPOSE_LOCAL) port cowork 8090 2>/dev/null)" = 127.0.0.1:$(COWORK_API_PORT) \
		&& test "$$($(COMPOSE_LOCAL) port cowork 8092 2>/dev/null | sed 's/.*://')" = $(COWORK_USERCONTENT_PORT) \
		|| { echo "e2e: cowork is not published on localhost:$(COWORK_API_PORT) and :$(COWORK_USERCONTENT_PORT) — run make -C cowork-web stack-up" >&2; exit 1; }
	pnpm exec playwright install chromium
	GARAM_API_URL=http://$(API_HOST_ADDR) GARAM_E2E_DATABASE_URL='$(WEB_E2E_DATABASE_URL)' pnpm e2e
