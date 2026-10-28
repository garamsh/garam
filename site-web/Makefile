# site — the landing page and the way in (ADR-0131), a Next.js app
# (ADR-0009). It installs from the workspace's lockfile at the root. Its
# verbs are every module's (#1661); `make -C site-web ci` is its fast gate,
# and `stack-up suites` the half that needs Docker.

include ../tooling/make/stack.mk

.PHONY: ci install format lint test build stack-up suites e2e

ci: install format lint test build

# install pins the install to the lockfile, which pnpm would otherwise
# resolve itself. The gate runs it first, so a gate never reads a tree
# its lockfile does not describe.
install:
	pnpm install --frozen-lockfile

# eslint prints nothing and exits 0 both when it covered the tree and
# when its ignore patterns left it a fraction of it, so what it covered
# is read out of its own JSON report: one entry per file it linted. A
# report naming no file fails the target below rather than reporting a
# count nobody checked. One pass writes the report, and the same report
# is printed through eslint's own stylish formatter, so the count and
# the lint result come from one run whose exit status the target keeps
# (#1528).
LINT_REPORT = const [file, status] = process.argv.slice(1); \
	const text = require("fs").readFileSync(file, "utf8"); \
	if (!text) { console.error("lint: eslint exited " + status + " and wrote no report"); process.exit(1); } \
	const results = JSON.parse(text); \
	console.log("lint: " + results.length + " files (eslint)"); \
	if (!results.length) { console.error("lint: nothing to lint — the eslint report named no file"); process.exit(1); } \
	new (require("eslint").ESLint)().loadFormatter("stylish").then((f) => process.stdout.write(f.format(results)));

# tsc reads its inputs from tsconfig.json rather than from eslint's
# ignore patterns, so the TS18003 it raises on its own empty input says
# nothing about what eslint read. The count above is the eslint half
# reporting for itself.
lint:
	@report=$$(mktemp) && trap 'rm -f "$$report"' EXIT; \
		status=0; pnpm exec eslint --format json --output-file "$$report" || status=$$?; \
		pnpm exec node -e '$(LINT_REPORT)' "$$report" "$$status" || exit 1; \
		exit "$$status"
	pnpm typecheck

# prettier reports no count and exits 0 when its ignore files leave it
# nothing, so what it opens is asked of prettier one path at a time. Its
# debug log is not that ask: prettier colours the `[debug]` prefix
# whenever CI is set in the environment, and those escape sequences left
# the scrape that read it matching nothing on every runner (#822).
#
# The candidates are what git has under site-web/ — tracked, plus
# untracked files no ignore file excludes — the set prettier's own
# default --ignore-path is aligned to.
FORMAT_CANDIDATES = $(shell git ls-files --cached --others --exclude-standard)

# getFileInfo is prettier's documented answer for one path: `ignored`
# applies both ignore files, and a null `inferredParser` is a file it
# skips, so an ignore file that swallowed every source cannot pass on a
# count of skipped files (#828). Each path is quoted for the App
# Router's parentheses and brackets, --silent keeps pnpm's install
# lines out of the paths, and the exit status is read because a crash
# prints nothing.
FORMAT_FILES = $(shell pnpm --silent exec node -e 'const p=require("prettier");(async()=>{for(const f of process.argv.slice(1)){const i=await p.getFileInfo(f,{ignorePath:[".gitignore",".prettierignore"]});if(!i.ignored&&i.inferredParser)console.log(f)}})()' $(patsubst %,'%',$(FORMAT_CANDIDATES)))$(if $(filter-out 0,$(.SHELLSTATUS)),$(error format: prettier could not be asked which files it checks — the getFileInfo run above exited $(.SHELLSTATUS)))

format:
	@echo 'format: $(words $(FORMAT_FILES)) files (prettier)'
	@test -n '$(FORMAT_CANDIDATES)' || { echo 'format: nothing to check — git lists no file under site-web/'; exit 1; }
	@test -n '$(FORMAT_FILES)' || { echo 'format: nothing to check — prettier called all $(words $(FORMAT_CANDIDATES)) of them ignored or unparseable'; exit 1; }
	@pnpm format

test:
	pnpm test

# `next build` runs with NODE_ENV=production, where env.ts requires
# GARAM_API_URL (the `/api/*` rewrite is baked into the build) and
# GARAM_CONSOLE_URL, which is read again at runtime. Nothing calls
# either during the build, so a placeholder is enough.
build:
	GARAM_API_URL=http://localhost:8080 GARAM_CONSOLE_URL=http://console.garam.localhost:3000 pnpm build

# The backend the browser suite drives: postgres, the one-shot
# migration, authz and api.
stack-up:
	@$(MAKE) --no-print-directory -C ../deploy/local backend-up

suites: install e2e

# e2e drives the built app in a real browser against the backend
# stack-up starts. It builds and serves the app itself, and the
# console beside it, since a session signed in on one is read on the
# other (ADR-0131). So it needs GARAM_API_URL — the real api here, not
# `build`'s placeholder — the URL its seed script writes sessions
# through, and the browser, which Playwright keeps outside the
# lockfile.
e2e:
	@test -n "$(API_HOST_ADDR)" || { echo "e2e: no api published on this host — run make -C site-web stack-up" >&2; exit 1; }
	@test -n '$(WEB_E2E_DATABASE_URL)' || { echo "e2e: no postgres published on this host — run make -C site-web stack-up" >&2; exit 1; }
	pnpm exec playwright install chromium
	GARAM_API_URL=http://$(API_HOST_ADDR) GARAM_E2E_DATABASE_URL='$(WEB_E2E_DATABASE_URL)' pnpm e2e
