# The checks of the garam-gaon chart (ADR-0155), included by
# deploy/charts/Makefile so `make -C deploy/charts ci` runs them with the
# garam chart's. Their terms are the garam chart's: the pinned helm, one
# render case per value file, every count printed and a zero count a
# failure. What each reaches and what it does not is the README's
# §What the checks are complete over.

.PHONY: gaon-lint gaon-render gaon-order gaon-fields gaon-guards gaon-names gaon-secrets

GAON_DIR := garam-gaon
GAON_TEMPLATES = $(wildcard $(GAON_DIR)/templates/*.yaml)
GAON_VALUES = $(wildcard $(GAON_DIR)/ci/*-values.yaml)
GAON_REFUSED = $(wildcard $(GAON_DIR)/ci/refused/*.yaml)

# The environment names the processes the chart runs read. The Go and
# policy sets are the garam chart's own sweeps; cowork's are the
# `env*` constants of its cli.go, which os.Getenv reads through once,
# and cowork-web's are its env.ts, parsed as the other two web apps' are
# but for a line that carries a default after the read.
GAON_COWORK_ENV = $(shell grep -hoE '^[[:space:]]*env[A-Za-z0-9]+[[:space:]]*= "COWORK_[A-Z0-9_]*"' ../../cowork/internal/cli/cli.go | sed 's/.*"\([A-Z][A-Z0-9_]*\)"/\1/' | sort -u)
GAON_ENV_TS = sed -n 's/^ *\([A-Z][A-Z0-9_]*\): process\.env\.\1\( .*\)\{0,1\},$$/\1/p'
GAON_CONSOLE_ENV = $(shell $(GAON_ENV_TS) ../../platform-web/src/env.ts | sort -u)
GAON_SITE_ENV = $(shell $(GAON_ENV_TS) ../../site-web/src/env.ts | sort -u)
GAON_COWORKWEB_ENV = $(shell $(GAON_ENV_TS) ../../cowork-web/src/env.ts | sort -u)
GAON_WEB_ENV = $(sort $(GAON_CONSOLE_ENV) $(GAON_SITE_ENV) $(GAON_COWORKWEB_ENV))

# What the chart declares it does not set, from the README table that
# carries the reasons.
GAON_UNSET_ENV = $(shell awk '/^\#\# /{s=0} /^\#\# Not set by this chart/{s=1} s' $(GAON_DIR)/README.md \
	| sed -n 's/^| `\([A-Z][A-Z0-9_]*\)`.*/\1/p')

gaon-lint:
	@echo 'gaon-lint: $(words $(GAON_TEMPLATES)) templates, $(words $(GAON_VALUES)) value files (helm lint --strict)'
	@test -n '$(GAON_TEMPLATES)' || { echo 'gaon-lint: nothing to lint — $(GAON_DIR)/templates holds no template'; exit 1; }
	@test -n '$(GAON_VALUES)' || { echo 'gaon-lint: nothing to lint — $(GAON_DIR)/ci holds no value file'; exit 1; }
	@set -e; for v in $(GAON_VALUES); do $(HELM) lint --strict --quiet $(GAON_DIR) --values "$$v"; done

# gaon-render renders every value file, compares the environment names
# the render carries against the ones the source reads, and holds every
# address a process dials to the full `.default.svc.<zone>` form, the
# only one gaon resolves (dns.md at the commit the README names).
gaon-render:
	@echo 'gaon-render: $(words $(CHART_GO_ENV)) platform, $(words $(GAON_COWORK_ENV)) cowork, $(words $(GAON_WEB_ENV)) web and $(words $(CHART_POLICY_ENV)) policy environment names read by the source, $(words $(GAON_UNSET_ENV)) declared unset — environment names and address form only, $(GAON_DIR)/README.md §What the checks are complete over'
	@test -n '$(CHART_GO_ENV)' && test -n '$(GAON_COWORK_ENV)' && test -n '$(GAON_WEB_ENV)' && test -n '$(CHART_POLICY_ENV)' \
		|| { echo 'gaon-render: the source sweep read no name — a parse of the envName block, cowork cli.go, an env.ts or policy config.rs is broken'; exit 1; }
	@# Every cowork read is os.Getenv, and it is called once, inside the
	@# helper the constants feed. A second read is a name the sweep cannot
	@# see.
	@reads="$$(grep -rhoE 'os\.(Getenv|LookupEnv|Environ)\(' --include=*.go --exclude=*_test.go ../../cowork | wc -l)"; \
		test "$$reads" -eq 1 \
		|| { echo "gaon-render: $$reads reads of the environment under cowork, expected the one cli.go holds — a read outside it is a name this check cannot see"; exit 1; }
	@test -n '$(GAON_UNSET_ENV)' \
		|| { echo 'gaon-render: $(GAON_DIR)/README.md §Not set by this chart named no variable — the parse is broken'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(GAON_VALUES); do $(HELM) template garam $(GAON_DIR) --values "$$v" > "$$out/$$(basename "$$v")"; done; \
		echo "gaon-render: $$(cat "$$out"/*-values.yaml | grep -c '^kind:') documents rendered from $(words $(GAON_VALUES)) value files"; \
		sed -n 's/^ *- name: \([A-Z][A-Z0-9_]*\)$$/\1/p' "$$out"/*-values.yaml | sort -u > "$$out/rendered"; \
		printf '%s\n' $(GAON_UNSET_ENV) | sort -u > "$$out/unset"; \
		printf '%s\n' $(CHART_GO_ENV) $(GAON_COWORK_ENV) $(GAON_WEB_ENV) $(CHART_POLICY_ENV) | sort -u > "$$out/source"; \
		both="$$(comm -12 "$$out/rendered" "$$out/unset")"; \
		test -z "$$both" || { echo "gaon-render: rendered and declared unset at once:"; echo "$$both"; exit 1; }; \
		for v in $(GAON_VALUES); do awk -f $(GAON_DIR)/ci/envs.awk "$$out/$$(basename "$$v")"; done | sort -u > "$$out/pairs"; \
		for p in platform:'$(CHART_GO_ENV)' cowork:'$(GAON_COWORK_ENV)' policy:'$(CHART_POLICY_ENV)' console:'$(GAON_CONSOLE_ENV)' site:'$(GAON_SITE_ENV)' coworkweb:'$(GAON_COWORKWEB_ENV)'; do \
			proc="$${p%%:*}"; \
			printf '%s\n' $${p#*:} | sort -u > "$$out/own"; \
			awk -v p="$$proc" '$$1 == p { print $$2 }' "$$out/pairs" > "$$out/theirs"; \
			test -s "$$out/theirs" || { echo "gaon-render: no variable rendered for $$proc — the object-name parse in ci/envs.awk is broken"; exit 1; }; \
			stray="$$(comm -23 "$$out/theirs" "$$out/own")"; \
			test -z "$$stray" || { echo "gaon-render: rendered on $$proc, and not read by the source of $$proc:"; echo "$$stray"; exit 1; }; \
		done; \
		stray="$$(comm -23 "$$out/rendered" "$$out/source")"; \
		test -z "$$stray" || { echo "gaon-render: rendered, and read by no source this check sweeps:"; echo "$$stray"; exit 1; }; \
		missing="$$(comm -13 "$$out/rendered" "$$out/source" | comm -23 - "$$out/unset")"; \
		test -z "$$missing" || { echo "gaon-render: read by the source, neither rendered nor declared unset:"; echo "$$missing"; exit 1; }; \
		stale="$$(comm -13 "$$out/source" "$$out/unset")"; \
		test -z "$$stale" || { echo "gaon-render: declared unset, and read by nothing:"; echo "$$stale"; exit 1; }; \
		echo "gaon-render: $$(wc -l < "$$out/rendered") names rendered, $$(wc -l < "$$out/unset") declared unset, $$(wc -l < "$$out/source") read"; \
		full="$$(grep -hE '^ +value: "(https?://)?[a-z0-9-]+\.default\.svc\.[a-z0-9.-]+:[0-9]+"$$' "$$out"/*-values.yaml | wc -l)"; \
		short="$$(grep -hE '^ +value: "(https?://)?[a-z0-9-]+:[0-9]+"$$' "$$out"/*-values.yaml || true)"; \
		test "$$full" -gt 0 || { echo 'gaon-render: no full Service name rendered — the address parse is broken'; exit 1; }; \
		test -z "$$short" || { echo 'gaon-render: an address names a Service by its short name, which gaon does not resolve:'; echo "$$short"; exit 1; }; \
		echo "gaon-render: $$full addresses name a Service in full, none in its short form"

# gaon-order holds each render to the kinds its stage owns and to the
# order Helm emits them in, which is the order `gaon apply` takes: a
# Service before the Deployment that selects it, and the routes after
# both. The migrate stage holds three Jobs and nothing else; a serve
# stage holds no Job. Counts are the contract's: eight Deployments and
# a Service each, three Jobs.
gaon-order:
	@echo 'gaon-order: $(words $(GAON_VALUES)) value files, each held to the kinds of its stage and to Helm install order'
	@test -n '$(GAON_VALUES)' || { echo 'gaon-order: nothing to check — $(GAON_DIR)/ci holds no value file'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(GAON_VALUES); do \
			n="$$(basename "$$v")"; \
			stage="$$(sed -n 's/^stage: *\([a-z]*\)$$/\1/p' "$$v")"; \
			$(HELM) template garam $(GAON_DIR) --values "$$v" > "$$out/render"; \
			sed -n 's/^kind: \(.*\)$$/\1/p' "$$out/render" > "$$out/kinds"; \
			count() { grep -cx "$$1" "$$out/kinds" || true; }; \
			echo "gaon-order: $$n — stage $$stage: $$(count Deployment) Deployment, $$(count Service) Service, $$(count Job) Job, $$(count Gateway) Gateway, $$(count HTTPRoute) HTTPRoute, $$(count TLSRoute) TLSRoute"; \
			case "$$stage" in \
			migrate) \
				test "$$(count Job)" -eq 3 && test "$$(wc -l < "$$out/kinds")" -eq 3 \
					|| { echo "gaon-order: $$n is the migrate stage and must render the three Jobs and nothing else"; exit 1; } ;; \
			serve) \
				test "$$(count Job)" -eq 0 && test "$$(count Deployment)" -eq 8 && test "$$(count Service)" -eq 8 \
					|| { echo "gaon-order: $$n is the serve stage and must render eight Deployments, eight Services and no Job"; exit 1; }; \
				awk '{ r = ($$0 == "Service" ? 1 : ($$0 == "Deployment" ? 2 : 3)); if (r < last) { print "gaon-order: " $$0 " follows a kind that Helm emits after it, line " NR; bad = 1 } last = r } END { exit bad }' "$$out/kinds" ;; \
			*) echo "gaon-order: $$n names no stage"; exit 1 ;; \
			esac; \
		done

# gaon-fields compares every field path the renders set against
# ci/fields.allow, both ways: a field the allowlist does not hold is one
# no gaon file was shown to state, and an entry no render sets is one
# nothing exercises. The allowlist is the PR's field-by-field comparison
# kept as data (README §How the rendered objects are checked).
gaon-fields:
	@echo 'gaon-fields: $(words $(GAON_VALUES)) value files rendered, field paths compared with $(GAON_DIR)/ci/fields.allow — field names only, not their values or types'
	@test -n '$(GAON_VALUES)' || { echo 'gaon-fields: nothing to check — $(GAON_DIR)/ci holds no value file'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(GAON_VALUES); do $(HELM) template garam $(GAON_DIR) --values "$$v" > "$$out/render"; awk -f $(GAON_DIR)/ci/paths.awk "$$out/render" >> "$$out/paths"; done; \
		sort -u "$$out/paths" > "$$out/rendered"; \
		grep -v '^#' $(GAON_DIR)/ci/fields.allow | sort -u > "$$out/allowed"; \
		test -s "$$out/rendered" && test -s "$$out/allowed" || { echo 'gaon-fields: a side of the comparison is empty — the path parse or the allowlist is broken'; exit 1; }; \
		extra="$$(comm -23 "$$out/rendered" "$$out/allowed")"; \
		test -z "$$extra" || { echo 'gaon-fields: rendered, and no gaon file was shown to state it (ci/fields.allow):'; echo "$$extra"; exit 1; }; \
		unused="$$(comm -13 "$$out/rendered" "$$out/allowed")"; \
		test -z "$$unused" || { echo 'gaon-fields: allowed, and no render sets it:'; echo "$$unused"; exit 1; }; \
		echo "gaon-fields: $$(wc -l < "$$out/rendered") distinct field paths, all allowed, none unused"

# gaon-guards renders each values file under ci/refused/ over the
# minimal one and holds that helm refuses it with
# `execution error at (<template>): <value> `, naming the value the file
# is named after. The controls are the three value files, which must
# still render: a guard that refused everything would pass every case.
gaon-guards:
	@echo 'gaon-guards: $(words $(GAON_REFUSED)) values files that must be refused, each naming the value it is named after'
	@test -n '$(GAON_REFUSED)' || { echo 'gaon-guards: nothing to check — $(GAON_DIR)/ci/refused holds no values file'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for c in $(GAON_VALUES); do \
			$(HELM) template garam $(GAON_DIR) --values "$$c" >/dev/null \
				|| { echo "gaon-guards: the control render of $$c was refused — the guards refuse a chart that should install"; exit 1; }; \
		done; \
		for v in $(GAON_REFUSED); do \
			want="$$(basename "$$v" .yaml | sed 's/@.*//')"; \
			if $(HELM) template garam $(GAON_DIR) --values $(GAON_DIR)/ci/minimal-values.yaml --values "$$v" >/dev/null 2>"$$out/err"; then \
				echo "gaon-guards: $$v rendered, and the guard for $$want did not fire"; exit 1; \
			fi; \
			key="$$(printf %s "$$want" | sed 's/\./\\./g')"; \
			grep -qE "execution error at \([^)]*\): $$key( |\$$)" "$$out/err" \
				|| { echo "gaon-guards: $$v was refused, but not by the guard for $$want — a refusal for another reason, or one whose message does not open with the value:"; cat "$$out/err"; exit 1; }; \
		done; \
		echo "gaon-guards: $(words $(GAON_REFUSED)) refused as named, and $(words $(GAON_VALUES)) control renders still pass"

# gaon-names holds every object name to 63 characters under a long
# prefix, and holds the prefix a name cannot carry to a refusal naming
# it. A render does not enforce the limit an apply does (#725, #732).
GAON_LONG_PREFIX := garam-production-eu-west-1-aa
GAON_TOO_LONG_PREFIX := garam-production-eu-west-1-primary-tenant-alpha-blue-green

gaon-names:
	@echo 'gaon-names: $(words $(GAON_VALUES)) value files rendered under prefix $(GAON_LONG_PREFIX) ($(shell printf %s '$(GAON_LONG_PREFIX)' | wc -c) characters) — object names only, $(GAON_DIR)/README.md §What the checks are complete over'
	@test -n '$(GAON_VALUES)' || { echo 'gaon-names: nothing to render — $(GAON_DIR)/ci holds no value file'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(GAON_VALUES); do \
			$(HELM) template garam $(GAON_DIR) --values "$$v" --set namePrefix=$(GAON_LONG_PREFIX) > "$$out/render"; \
			sed -n 's/^  name: \(.*\)$$/\1/p' "$$out/render" >> "$$out/names"; \
		done; \
		total="$$(sort -u "$$out/names" | wc -l)"; \
		test "$$total" -gt 0 || { echo 'gaon-names: the render named no object — the name parse is broken'; exit 1; }; \
		over="$$(awk 'length($$0) > 63 { print length($$0)" "$$0 }' "$$out/names" | sort -u)"; \
		test -z "$$over" || { echo 'gaon-names: over the 63-character limit, which an apply refuses and a render does not:'; echo "$$over"; exit 1; }; \
		if $(HELM) template garam $(GAON_DIR) --values $(GAON_DIR)/ci/minimal-values.yaml --set namePrefix=$(GAON_TOO_LONG_PREFIX) >/dev/null 2>"$$out/err"; then \
			echo 'gaon-names: a prefix of $(shell printf %s '$(GAON_TOO_LONG_PREFIX)' | wc -c) characters rendered'; exit 1; fi; \
		grep -qE 'execution error at \([^)]*\): namePrefix ' "$$out/err" \
			|| { echo 'gaon-names: the long prefix was refused, but not by the guard for namePrefix:'; cat "$$out/err"; exit 1; }; \
		echo "gaon-names: $$total distinct object names, longest $$(awk '{ if (length($$0) > m) m = length($$0) } END { print m }' "$$out/names"), none over 63; a prefix of $(shell printf %s '$(GAON_TOO_LONG_PREFIX)' | wc -c) characters refused by name"

# gaon-secrets holds that no secret value is in a rendered object or in
# the chart's files. Rendered, a Secret or ConfigMap is a kind the chart
# does not own, and an environment variable that carries a credential
# is a reference. In the files, nothing has the shape of a PEM block or
# a URL with a password. Both are shapes: a credential in no such shape
# is not one this check reads (README §What the checks are complete
# over).
gaon-secrets:
	@echo 'gaon-secrets: $(words $(GAON_VALUES)) value files rendered, and every file under $(GAON_DIR) searched for a PEM block or a URL with a password'
	@test -n '$(GAON_VALUES)' || { echo 'gaon-secrets: nothing to check — $(GAON_DIR)/ci holds no value file'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(GAON_VALUES); do $(HELM) template garam $(GAON_DIR) --values "$$v" >> "$$out/render"; done; \
		own="$$(grep -E '^kind: (Secret|ConfigMap)$$' "$$out/render" || true)"; \
		test -z "$$own" || { echo 'gaon-secrets: the chart renders a Secret or a ConfigMap, and creates none (ADR-0042):'; echo "$$own"; exit 1; }; \
		awk '/^ *- name: [A-Z0-9_]*(DATABASE_URL|SECRET|_KEY)$$/ { n++; want = NR + 1; next } NR == want { if ($$0 !~ /^ +valueFrom:$$/) { print "gaon-secrets: line " (NR - 1) " sets a credential without a reference: " $$0; bad = 1 } else ok++ } END { if (n == 0) { print "gaon-secrets: no credential variable found — the parse is broken"; exit 1 } print "gaon-secrets: " ok " credential variables, each a reference"; exit bad }' "$$out/render"; \
		found="$$(grep -rnE 'BEGIN [A-Z ]*(PRIVATE KEY|CERTIFICATE)|://[^/@ ]+:[^/@ ]+@' $(GAON_DIR) || true)"; \
		test -z "$$found" || { echo 'gaon-secrets: a PEM block or a URL with a password is in the chart:'; echo "$$found"; exit 1; }; \
		echo "gaon-secrets: $$(find $(GAON_DIR) -type f | wc -l) files searched, none holds one"
