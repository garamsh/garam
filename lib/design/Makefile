# lib/design — the design system both web apps import (ADR-0125): the
# primitives, the components every app shares, and the stylesheet. It
# ships TypeScript source, which each app compiles. Its verbs are every
# module's (#1661).

.PHONY: ci install format lint test

ci: install format lint test

install:
	pnpm install --frozen-lockfile

FILES = $(shell git ls-files --cached --others --exclude-standard)

lint:
	@echo 'lint: $(words $(filter %.ts %.tsx %.mjs %.mts,$(FILES))) files under lib/design (eslint, tsc)'
	@test -n '$(filter %.ts %.tsx,$(FILES))' || { echo 'lint: nothing to lint — git lists no TypeScript under lib/design'; exit 1; }
	pnpm lint
	pnpm typecheck

format:
	@echo 'format: $(words $(FILES)) files under lib/design (prettier, less its ignore files)'
	@test -n '$(FILES)' || { echo 'format: nothing to check — git lists no file under lib/design'; exit 1; }
	pnpm format

test:
	pnpm test
