# deploy/charts — the chart that describes a garam deployment
# (ADR-0042). Its verbs are every module's (#1661): `make -C
# deploy/charts ci` is the whole check, and each half is a named task
# of its own. helm is run through the pin below rather than off the
# path, so a contributor installs nothing and every run takes one
# version.

include ../../tooling/make/go.mk

.PHONY: ci chart-lint chart-render chart-identity chart-names chart-rewrite chart-guards chart-security chart-package

# helm is not a tool directive: helm.sh/helm/v4 requires a
# google.golang.org/protobuf newer than protoc-gen-go's, and merging its
# graph into platform/go.mod raises that floor and restamps every
# generated .pb.go header. Nothing parses this line for a pin, so no bot
# proposes a bump and no scheduled review is named; naming no mover of
# its own, it is the PM's to move under AGENTS.md §Roles. It writes no
# output this repository commits, so nothing here fails when it goes
# stale.
HELM := go run helm.sh/helm/v4/cmd/helm@v4.2.4

CHART_DIR := garam
CHART_TEMPLATES = $(wildcard $(CHART_DIR)/templates/*.yaml)

# One render case per value file: the minimal one takes every default
# and the full one sets every optional value, so only their union
# reaches the templates' one branch.
CHART_VALUES = $(wildcard $(CHART_DIR)/ci/*-values.yaml)

# Every environment variable garam reads, from its declarations: the
# envName block in platform/internal/cli, and the console's and the
# site's src/env.ts parsed for their own shape. The guards chart-render
# runs first keep the block equal to the reads (#734), and both sides
# read one character class,
# since a name outside it drops out of both sets and passes unseen
# (#864).
CHART_GO_ENV = $(shell grep -rhoE 'envName = "[A-Z][A-Z0-9_]*"' --include=*.go ../../platform/internal ../../platform/cmd | sed 's/.*"\([A-Z][A-Z0-9_]*\)"/\1/' | sort -u)
CHART_WEB_ENV = $(shell sed -n 's/^ *\([A-Z][A-Z0-9_]*\): process\.env\.\1,$$/\1/p' ../../console/src/env.ts ../../site/src/env.ts | sort -u)

# What the chart declares it does not set, read from the table that
# carries the reasons. A renamed heading empties this and fails the
# target below rather than exempting nothing quietly.
CHART_UNSET_ENV = $(shell awk '/^\#\# /{s=0} /^\#\# Not set by this chart/{s=1} s' $(CHART_DIR)/README.md \
	| sed -n 's/^| `\([A-Z][A-Z0-9_]*\)`.*/\1/p')

# Every workload the chart renders, the kind it renders as, and the
# value that names the account it runs as — read from the README table a
# consumer places its own names through. A renamed heading empties this
# and fails the target below rather than checking nothing.
CHART_IDENTITIES = $(shell awk '/^\#\# /{s=0} /^\#\# Workload identities/{s=1} s' $(CHART_DIR)/README.md \
	| sed -n 's/^| `\([a-z0-9-]*\)` | \([A-Za-z]*\) | `\([a-zA-Z0-9.]*\)` |.*/\1=\2=\3/p')

# The value file that names no account, so its render is what a
# deployment supplying only what the chart requires receives.
CHART_DEFAULT_VALUES = $(CHART_DIR)/ci/minimal-values.yaml

# The resource names the packaged artifact may not carry as a key,
# listed so the count the target prints is the set it searched for. Two
# names, which $(CHART_DIR)/README.md §What the checks are complete over
# records as the whole of what that target reaches.
CHART_RESOURCE_NAMES := cpu memory

ci: chart-lint chart-render chart-identity chart-names chart-rewrite chart-guards chart-security chart-package
chart-lint:
	@echo 'chart-lint: $(words $(CHART_TEMPLATES)) templates, $(words $(CHART_VALUES)) value files (helm lint --strict)'
	@test -n '$(CHART_TEMPLATES)' || { echo 'chart-lint: nothing to lint — $(CHART_DIR)/templates holds no template'; exit 1; }
	@test -n '$(CHART_VALUES)' || { echo 'chart-lint: nothing to lint — $(CHART_DIR)/ci holds no value file'; exit 1; }
	@set -e; for v in $(CHART_VALUES); do $(HELM) lint --strict --quiet $(CHART_DIR) --values "$$v"; done

# chart-render renders every value file and compares the environment
# names the render carries against the ones the source reads. Nothing
# else keeps the chart honest: a manifest that renders is not a manifest
# that matches the code. Every container, port and volume name in the
# chart is lowercase, so an all-caps `- name:` in the render is an
# environment variable.
chart-render:
	@echo 'chart-render: $(words $(CHART_GO_ENV)) Go and $(words $(CHART_WEB_ENV)) web environment names read by the source, $(words $(CHART_UNSET_ENV)) declared unset — environment names only, $(CHART_DIR)/README.md §What the checks are complete over'
	@test -n '$(CHART_GO_ENV)' && test -n '$(CHART_WEB_ENV)' \
		|| { echo 'chart-render: the source sweep read no name — the envName block or the env.ts parse is broken'; exit 1; }
	@# What makes the block above the whole of what the Go source reads:
	@# every read reaches os.Getenv, and os.Getenv is called once, inside
	@# envOr. A second read is a name this check cannot see.
	@reads="$$(grep -rhoE 'os\.(Getenv|LookupEnv|Environ)\(' --include=*.go --exclude=*_test.go ../../platform/internal ../../platform/cmd | wc -l)"; \
		test "$$reads" -eq 1 \
		|| { echo "chart-render: $$reads reads of the environment under platform/internal|cmd, expected the one envOr holds — a read outside it is a name this check cannot see"; exit 1; }
	@# And every name reaching envOr is declared rather than written at
	@# the call, so no reader can name a variable this check does not
	@# enumerate. An env-shaped literal passed to any call is that.
	@stray="$$(grep -rnE '[A-Za-z_][A-Za-z0-9_]*\("(GARAM|GOOGLE)_[A-Z_]+"' --include=*.go --exclude=*_test.go ../../platform/internal ../../platform/cmd || true)"; \
		test -z "$$stray" \
		|| { echo 'chart-render: an environment name is written at a call site rather than declared in platform/internal/cli, so this check cannot enumerate it:'; echo "$$stray"; exit 1; }
	@test -n '$(CHART_UNSET_ENV)' \
		|| { echo 'chart-render: $(CHART_DIR)/README.md §Not set by this chart named no variable — the parse is broken'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(CHART_VALUES); do $(HELM) template garam $(CHART_DIR) --values "$$v" > "$$out/$$(basename "$$v")"; done; \
		echo "chart-render: $$(cat "$$out"/*-values.yaml | grep -c '^kind:') documents rendered"; \
		sed -n 's/^ *- name: \([A-Z][A-Z0-9_]*\)$$/\1/p' "$$out"/*-values.yaml | sort -u > "$$out/rendered"; \
		printf '%s\n' $(CHART_UNSET_ENV) | sort -u > "$$out/unset"; \
		printf '%s\n' $(CHART_GO_ENV) $(CHART_WEB_ENV) | sort -u > "$$out/source"; \
		both="$$(comm -12 "$$out/rendered" "$$out/unset")"; \
		test -z "$$both" || { echo "chart-render: rendered and declared unset at once:"; echo "$$both"; exit 1; }; \
		sort -u "$$out/rendered" "$$out/unset" > "$$out/covered"; \
		diff "$$out/source" "$$out/covered" \
			|| { echo 'chart-render: < read by the source, neither rendered nor declared unset; > covered by the chart, read by nothing'; exit 1; }

# chart-identity compares the identity every rendered workload runs as
# against the table the README declares it under, under two release
# names. An account name is the subject another repository binds a role
# to (ADR-0043): a name this chart composed would move with the release
# name, the credential would stop arriving at runtime, and one render
# cannot see it. Keyed on the component label, which no release name
# enters, so the two renders compare directly.
chart-identity:
	@echo 'chart-identity: $(words $(CHART_IDENTITIES)) workloads declared by $(CHART_DIR)/README.md §Workload identities — pod identity only, §What the checks are complete over'
	@test -n '$(CHART_IDENTITIES)' \
		|| { echo 'chart-identity: $(CHART_DIR)/README.md §Workload identities declared no workload — the parse is broken'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		identity() { rel="$$1"; shift; $(HELM) template "$$rel" $(CHART_DIR) "$$@" > "$$out/raw"; awk ' \
			function flush() { \
				if (kind == "Deployment" || kind == "Job") printf "workload %s=%s runs as %s\n", comp, kind, \
					(fields > 1 ? fields " serviceAccountName fields at once" : (sa == "" ? "the namespace default" : sa)); \
				else if (kind == "ServiceAccount") printf "account %s\n", name } \
			/^---$$/ { flush(); kind = ""; comp = ""; name = ""; sa = ""; fields = 0; next } \
			/^kind: / { kind = $$2 } \
			/^  name: / { if (name == "") name = $$2 } \
			/app.kubernetes.io\/component: / { if (comp == "") comp = $$2 } \
			/^ *serviceAccountName: / { sa = $$2; fields++ } \
			END { flush() }' "$$out/raw" | sort; }; \
		printf '%s\n' $(CHART_IDENTITIES) | cut -d= -f1,2 | sort -u > "$$out/declared"; \
		for v in $(CHART_VALUES); do \
			n="$$(basename "$$v")"; \
			identity garam --values "$$v" > "$$out/garam"; \
			identity prod-garam --values "$$v" > "$$out/prod-garam"; \
			echo "chart-identity: $$n — $$(grep -c '^workload ' "$$out/garam" || true) workloads, $$(grep -c '^account ' "$$out/garam" || true) accounts"; \
			diff "$$out/garam" "$$out/prod-garam" \
				|| { echo "chart-identity: $$n: < release garam, > release prod-garam — an identity moved with the release name, and so did the subject another repository binds a role to"; exit 1; }; \
			sed -n 's/^workload \([a-zA-Z0-9=-]*\) runs as .*/\1/p' "$$out/garam" | sort -u > "$$out/rendered"; \
			diff "$$out/declared" "$$out/rendered" \
				|| { echo "chart-identity: $$n: < workload=kind declared in the README, > rendered by the chart"; exit 1; }; \
			sed -n 's/^account //p' "$$out/garam" | sort -u > "$$out/accounts"; \
			sed -n 's/^workload .* runs as //p' "$$out/garam" | sort -u > "$$out/bound"; \
			unused="$$(comm -23 "$$out/accounts" "$$out/bound")"; \
			test -z "$$unused" || { echo "chart-identity: $$n renders an account no workload runs as — a subject the next reader takes for one that was needed, and one an annotation sends a consumer to a role for:"; echo "$$unused"; exit 1; }; \
		done; \
		probe=""; for d in $(CHART_IDENTITIES); do probe="$$probe --set $${d##*=}=probe-$${d%%=*}"; done; \
		identity garam --values $(CHART_DEFAULT_VALUES) $$probe > "$$out/probe"; \
		for d in $(CHART_IDENTITIES); do \
			grep -q " runs as probe-$${d%%=*}$$" "$$out/probe" \
				|| { echo "chart-identity: §Workload identities names $${d##*=} for $${d%%=*}, and setting it named no account — the value is not the one the chart reads, or it is not read verbatim"; exit 1; }; \
		done; \
		identity garam --values $(CHART_DEFAULT_VALUES) > "$$out/default"; \
		defaulted="$$(grep '^account ' "$$out/default" || true; grep '^workload ' "$$out/default" | grep -v 'runs as the namespace default' || true)"; \
		test -z "$$defaulted" || { echo "chart-identity: $$(basename $(CHART_DEFAULT_VALUES)) names no account and the chart supplied one anyway — the default is none:"; echo "$$defaulted"; exit 1; }; \
		echo "chart-identity: $$(basename $(CHART_DEFAULT_VALUES)) — no name given, so no account created and every workload on the namespace default"; \
		for d in $(CHART_IDENTITIES); do p="$${d##*=}"; \
			if $(HELM) template garam $(CHART_DIR) --values $(CHART_DEFAULT_VALUES) --set "$${p%.name}.create=true" >/dev/null 2>&1; then \
				echo "chart-identity: $${p%.name}.create=true rendered with no $$p — the chart composed a name, and a composed name moves the subject another repository binds a role to"; exit 1; fi; \
		done

# chart-names renders under the longest release name Helm accepts and
# holds every object name to 63 characters, which an apply enforces and
# a render does not (#725, #732). `^  name: ` is an object's own name,
# and each render lands in a file before it is parsed, so a failed
# render fails set -e rather than contributing no name (#864).
CHART_LONG_RELEASE := garam-production-eu-west-1-primary-tenant-alpha-01234

chart-names:
	@echo 'chart-names: $(words $(CHART_VALUES)) value files rendered as $(CHART_LONG_RELEASE) (release length $(shell printf %s '$(CHART_LONG_RELEASE)' | wc -c), Helm caps it at 53) — object names only, $(CHART_DIR)/README.md §What the checks are complete over'
	@test -n '$(CHART_VALUES)' || { echo 'chart-names: nothing to render — $(CHART_DIR)/ci holds no value file'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(CHART_VALUES); do \
			$(HELM) template $(CHART_LONG_RELEASE) $(CHART_DIR) --values "$$v" > "$$out/render"; \
			sed -n 's/^  name: \(.*\)$$/\1/p' "$$out/render" >> "$$out/names"; \
		done; \
		total="$$(sort -u "$$out/names" | wc -l)"; \
		test "$$total" -gt 0 || { echo 'chart-names: the render named no object — the name parse is broken'; exit 1; }; \
		over="$$(awk 'length($$0) > 63 { print length($$0)" "$$0 }' "$$out/names" | sort -u)"; \
		test -z "$$over" || { echo 'chart-names: over the 63-character limit, which an apply refuses and a render does not:'; echo "$$over"; exit 1; }; \
		echo "chart-names: $$total distinct object names, longest $$(awk '{ if (length($$0) > m) m = length($$0) } END { print m }' "$$out/names"), none over 63"

# chart-rewrite holds the `/api/*` target a release bakes into the web
# image against the api Service rendered under release `garam`, two
# facts nothing else reads together (#861). The render under
# $(CHART_LONG_RELEASE) is the control and must differ, or the equality
# proves nothing (ADR-0056). tooling/image-build.sh reads the same file
# for what the release builds.
CHART_BAKED_API_URL = $(shell sed -n 's/^ *GARAM_API_URL=\(.*\)$$/\1/p' ../../.github/workflows/release.yml)

chart-rewrite:
	@echo 'chart-rewrite: $(CHART_BAKED_API_URL), baked by .github/workflows/release.yml — one URL against the api Service, $(CHART_DIR)/README.md §What the checks are complete over'
	@test '$(words $(CHART_BAKED_API_URL))' = 1 \
		|| { echo 'chart-rewrite: .github/workflows/release.yml bakes $(words $(CHART_BAKED_API_URL)) GARAM_API_URL values, expected the one a release is bound to — the parse is broken'; exit 1; }
	@set -e; \
		apiurl() { $(HELM) template "$$1" $(CHART_DIR) --values $(CHART_DEFAULT_VALUES) | awk ' \
			function flush() { if (kind == "Service" && comp == "api") printf "http://%s:%s\n", name, port } \
			/^---$$/ { flush(); kind = ""; comp = ""; name = ""; port = ""; next } \
			/^kind: / { kind = $$2 } \
			/^  name: / { if (name == "") name = $$2 } \
			/app.kubernetes.io\/component: / { if (comp == "") comp = $$2 } \
			$$1 == "port:" { if (port == "") port = $$2 } \
			END { flush() }'; }; \
		rendered="$$(apiurl garam)"; \
		test -n "$$rendered" && test "$$(printf '%s\n' "$$rendered" | wc -l)" -eq 1 \
			|| { echo "chart-rewrite: the render named no single api Service — the parse is broken:"; echo "$$rendered"; exit 1; }; \
		test "$$rendered" = '$(CHART_BAKED_API_URL)' \
			|| { echo "chart-rewrite: the release bakes $(CHART_BAKED_API_URL) and release garam renders $$rendered — a published web image reaches a name this chart does not render, and nothing after the install says so"; exit 1; }; \
		control="$$(apiurl $(CHART_LONG_RELEASE))"; \
		test "$$control" != '$(CHART_BAKED_API_URL)' \
			|| { echo "chart-rewrite: release $(CHART_LONG_RELEASE) renders $$control as well, so the equality above holds under every release name and this check reads nothing"; exit 1; }; \
		echo "chart-rewrite: release garam renders $$rendered; release $(CHART_LONG_RELEASE) renders $$control, which no published web image reaches"

# chart-guards renders each values file under ci/refused/ and holds that
# helm refuses it with `execution error at (<template>): <value> `,
# naming the value the file is named after (#726, #740). The message is
# matched rather than the status or the file's path, which carries the
# same string (#864). The last render is the control:
# minimal-values.yaml must still render.
CHART_REFUSED = $(wildcard $(CHART_DIR)/ci/refused/*.yaml)

chart-guards:
	@echo 'chart-guards: $(words $(CHART_REFUSED)) values files that must be refused, each naming the value it is named after'
	@test -n '$(CHART_REFUSED)' || { echo 'chart-guards: nothing to check — $(CHART_DIR)/ci/refused holds no values file'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(CHART_REFUSED); do \
			want="$$(basename "$$v" .yaml)"; \
			if $(HELM) template garam $(CHART_DIR) --values $(CHART_DIR)/ci/minimal-values.yaml --values "$$v" >/dev/null 2>"$$out/err"; then \
				echo "chart-guards: $$v rendered, and the guard for $$want did not fire"; exit 1; \
			fi; \
			key="$$(printf %s "$$want" | sed 's/\./\\./g')"; \
			grep -qE "execution error at \([^)]*\): $$key( |\$$)" "$$out/err" \
				|| { echo "chart-guards: $$v was refused, but not by the guard for $$want — a refusal for another reason, or one whose message does not open with the value:"; cat "$$out/err"; exit 1; }; \
		done; \
		$(HELM) template garam $(CHART_DIR) --values $(CHART_DIR)/ci/minimal-values.yaml >/dev/null \
			|| { echo 'chart-guards: the control render was refused too — the guards refuse a chart that should install'; exit 1; }; \
		echo "chart-guards: $(words $(CHART_REFUSED)) refused as named, and minimal-values.yaml still renders"

chart-security:
	@CHART_DIR='$(CHART_DIR)' HELM='$(HELM)' \
		CHART_IDENTITIES='$(CHART_IDENTITIES)' CHART_VALUES='$(CHART_VALUES)' \
		../../tooling/chart-security.sh

# chart-package searches the artifact `helm package` produces, which no
# other check opens, for a resource name set as a key: no figure is one
# garam measured (deployment-contract.md §Not established). Its zero
# guard counts lines setting `resources:`, not lines holding the word,
# which this chart's README carries in prose (#582, #864).
chart-package:
	@echo 'chart-package: $(words $(CHART_RESOURCE_NAMES)) resource names searched for as keys in the packaged artifact — resource quantities only, $(CHART_DIR)/README.md §What the checks are complete over'
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		$(HELM) package $(CHART_DIR) --destination "$$out" >/dev/null; \
		tgz="$$(echo "$$out"/*.tgz)"; \
		mkdir "$$out/unpacked"; tar -xzf "$$tgz" -C "$$out/unpacked"; \
		cd "$$out/unpacked"; \
		files="$$(find . -type f | wc -l)"; \
		named="$$(grep -rnE '^[[:space:]]*resources:' . | wc -l)"; \
		test "$$files" -gt 0 && test "$$named" -gt 0 \
			|| { echo 'chart-package: the unpacked artifact sets no resources key — the package or the search read nothing'; exit 1; }; \
		re="$$(echo '$(CHART_RESOURCE_NAMES)' | tr ' ' '|')"; \
		found="$$(grep -rnE "\b($$re):" . || true)"; \
		test -z "$$found" || { echo "chart-package: the artifact sets a resource name, and no figure is published for any garam process (docs/architecture/deployment-contract.md §Not established):"; echo "$$found"; exit 1; }; \
		echo "chart-package: $$(basename "$$tgz") — $$files files, $$named lines setting a resources key, 0 setting one of the names"

