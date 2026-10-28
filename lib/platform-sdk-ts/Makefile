# lib/platform-sdk-ts — the platform's client for the web apps
# (ADR-0125): the calls an app binds to its backend and its sign-in, the
# API types generated from platform/api/openapi.yaml, and the sentences
# a refusal reads as. It ships TypeScript source, which each app
# compiles. Its verbs are every module's (#1661).

.PHONY: ci install format lint test gen check-gen

ci: install format lint test check-gen

install:
	pnpm install --frozen-lockfile

FILES = $(shell git ls-files --cached --others --exclude-standard)

lint:
	@echo 'lint: $(words $(filter %.ts %.mjs %.mts,$(FILES))) files under lib/platform-sdk-ts (eslint, tsc)'
	@test -n '$(filter %.ts,$(FILES))' || { echo 'lint: nothing to lint — git lists no TypeScript under lib/platform-sdk-ts'; exit 1; }
	pnpm lint
	pnpm typecheck

format:
	@echo 'format: $(words $(FILES)) files under lib/platform-sdk-ts (prettier, less its ignore files)'
	@test -n '$(FILES)' || { echo 'format: nothing to check — git lists no file under lib/platform-sdk-ts'; exit 1; }
	pnpm format

test:
	pnpm test

# gen regenerates the API types every web app reads the platform
# through, from its OpenAPI spec, which stays the contract's source of
# truth.
gen:
	pnpm gen:api

# What gen:api writes, read from the script that writes it: package.json
# owns the -o path, and a second copy here is the copy that goes stale.
GEN_PATHS = $(shell sed -n 's|.*"gen:api": ".* -o \([^" ]*\)".*|\1|p' package.json)

# A git diff pathspec matching no tracked file exits 0 having compared
# nothing, so what git tracks under the owned path is counted first: a
# moved -o or a removed file fails here instead of passing forever.
check-gen: gen
	@echo 'check-gen: $(words $(GEN_PATHS)) generated paths compared'
	@test -n '$(GEN_PATHS)' && git ls-files --error-unmatch -- $(GEN_PATHS) >/dev/null 2>&1 \
		|| { echo 'check-gen: nothing to compare — package.json names no gen:api output, or git tracks none of "$(GEN_PATHS)"'; exit 1; }
	@git diff --exit-code -- $(GEN_PATHS)

