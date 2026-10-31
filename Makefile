# The repository's entry points over every module (#1661). Each module
# keeps its own Makefile answering the same verbs —
# `make -C platform-web e2e`, `make -C cowork-web e2e`,
# `make -C platform integration` — and this file runs a verb in every
# module defining it, picks the modules a change can affect, and holds
# the few tasks that span modules: the release images and the
# repository's own utilities.

include tooling/make/stack.mk

# The modules, in the order tooling/ci/modules lists them, and the
# ones whose Makefile defines a verb. A module leaves out a verb it has
# no use for rather than defining an empty one.
MODULES := $(shell sed 's/\#.*//' tooling/ci/modules | awk 'NF {print $$1}')
modules-with = $(strip $(foreach m,$(MODULES),$(if $(shell grep -q '^$(1):' $(m)/Makefile && echo y),$(m))))

# Every verb a module answers, run in each module defining it, in table
# order. `ci` is the whole set: every module's fast gate, Docker-free.
# `stack-up` brings up what each module's suites need and `suites` runs
# them; `install` installs every web package from its lockfile.
VERBS := ci install format lint test build gen check-gen stack-up suites integration

.PHONY: $(VERBS) affected e2e web-e2e cowork-web-e2e db-up db-down db-prune backend-up
.PHONY: template-diff search image-build web-image-e2e site-image-e2e cowork-image-e2e cowork-web-image-e2e policy-image policy-image-e2e test-image test-image-e2e test-image-push test-image-pin

$(VERBS):
	@test -n '$(call modules-with,$@)' || { echo '$@: no module defines it — tooling/ci/modules or a module Makefile is misread'; exit 1; }
	@echo '$@: $(words $(call modules-with,$@)) modules — $(call modules-with,$@)'
	@set -e; for m in $(call modules-with,$@); do $(MAKE) --no-print-directory -C $$m $@; done

# affected prints the modules a change since BASE can affect, each with
# its reason; tooling/ci/affected.sh states how it chooses. CI runs the
# same script to pick its jobs.
affected:
	@test -n '$(BASE)' || { echo 'affected: name the base, as make affected BASE=origin/dev' >&2; exit 1; }
	@tooling/ci/affected.sh '$(BASE)'

# The names these were run by before each module had its own Makefile,
# kept while CI's goal commands and the documents use them: the
# platform's binary suite, and the browser suites. `web-e2e` was the
# one web app's and is now both of the apps it split into (ADR-0131),
# the console's and the site's.
e2e:
	@$(MAKE) --no-print-directory -C platform e2e

web-e2e:
	@$(MAKE) --no-print-directory -C platform-web e2e
	@$(MAKE) --no-print-directory -C site-web e2e

cowork-web-e2e:
	@$(MAKE) --no-print-directory -C cowork-web e2e

# The stack's verbs, which deploy/local/Makefile holds.
db-up db-down db-prune backend-up:
	@$(MAKE) --no-print-directory -C deploy/local $@

# The template commit this repository has taken every improvement from,
# up to. Nothing can derive it, since an adapted file differs from the
# template by design, so the pull request that takes template content
# moves it in a diff a reviewer reads (#775, #777).
TEMPLATE_SYNCED := 3d0c87aaf8d363fc304df45a588928febc97097a

template-diff:
	@TEMPLATE_SYNCED='$(TEMPLATE_SYNCED)' tooling/template-diff.sh

# Q reaches awk through the environment and never through the command
# text, so a phrase holding a quote is searched for rather than run.
export SEARCH_Q = $(Q)

search:
	@tooling/search.sh

# image-build builds the five images a release publishes, each from
# what .github/workflows/release.yml says it is built from; nothing else
# builds one (#760, #850). IMAGE_REGISTRY makes it a development build
# of the same repositories in that registry, tagged IMAGE_TAG or HEAD's
# first twelve hex digits (ADR-0071, ADR-0134); the script refuses any
# other tag and pushes nothing.
IMAGE_BUILD_ARGS = $(if $(IMAGE_REGISTRY),--registry '$(IMAGE_REGISTRY)') $(if $(IMAGE_TAG),--tag '$(IMAGE_TAG)') $(if $(IMAGE_BUILD_CACHE),--cache '$(IMAGE_BUILD_CACHE)')

image-build:
	tooling/image-build.sh $(IMAGE_BUILD_ARGS)

# What image-build tagged one repository's image, asked of the script
# that tags it, so a tag rule in release.yml cannot leave this naming an
# image an earlier run built (#870). Every image builds from the root,
# so each is told apart by its repository. A release that builds none
# into the repository stops the recipe reading this.
image_ref = $(or $(shell tooling/image-build.sh --names $(IMAGE_BUILD_ARGS) | awk '$$2 ~ /\/$(1):/ { print $$2 }'),$(error .github/workflows/release.yml builds no image into garam/$(1)))

# Each check's subject is the image image-build produced, the one a
# release publishes, rather than the app. GARAM_WEB_IMAGE, COWORK_IMAGE
# or COWORK_WEB_IMAGE names one to check instead, which is how an image
# pulled from ECR by digest is verified, and then nothing is built here.
# Named in one `make` call, as images.yml names them, they share one
# image-build.
#
# web-image-e2e and site-image-e2e: the standalone server starts,
# answers both probes and reaches the baked `/api/*` target.
web-image-e2e: $(if $(GARAM_WEB_IMAGE),,image-build)
	GARAM_WEB_IMAGE='$(or $(GARAM_WEB_IMAGE),$(call image_ref,console))' platform/tests/image/web.sh

site-image-e2e: $(if $(GARAM_WEB_IMAGE),,image-build)
	APP=site-web GARAM_WEB_IMAGE='$(or $(GARAM_WEB_IMAGE),$(call image_ref,site))' platform/tests/image/web.sh

# cowork-image-e2e and cowork-web-image-e2e: what each script states.
cowork-image-e2e: $(if $(COWORK_IMAGE),,image-build)
	COWORK_IMAGE='$(or $(COWORK_IMAGE),$(call image_ref,cowork))' tooling/$@.sh

cowork-web-image-e2e: $(if $(COWORK_WEB_IMAGE),,image-build)
	COWORK_WEB_IMAGE='$(or $(COWORK_WEB_IMAGE),$(call image_ref,cowork-web))' tooling/$@.sh

# policy-image-e2e: what its script states. No release publishes
# garam/policy yet, and image-build builds what a release publishes, so
# policy-image builds this check's subject itself. POLICY_IMAGE names
# one to check instead, and then nothing is built here.
POLICY_LOCAL_IMAGE := garam/policy:ci
comma := ,

policy-image-e2e: $(if $(POLICY_IMAGE),,policy-image)
	POLICY_IMAGE='$(or $(POLICY_IMAGE),$(POLICY_LOCAL_IMAGE))' tooling/$@.sh

policy-image:
	docker build -t $(POLICY_LOCAL_IMAGE) -f deploy/local/Dockerfile.policy $(if $(IMAGE_BUILD_CACHE),--load --cache-from type=$(IMAGE_BUILD_CACHE)$(comma)scope=garam/policy --cache-to type=$(IMAGE_BUILD_CACHE)$(comma)mode=max$(comma)scope=garam/policy) .

# test-image builds the test image another repository's CI copies
# garam, testprincipal and the migrations out of (ADR-0092), tagged
# with HEAD's full hash under TEST_IMAGE_REPOSITORY; the script refuses
# a working tree that differs from HEAD. test-image-e2e checks it as
# that consumer reads it. Both need Docker, so neither is in `ci`.
TEST_IMAGE_REPOSITORY ?= garam-test

test-image:
	tooling/image-test.sh '$(TEST_IMAGE_REPOSITORY)'

test-image-e2e: test-image
	platform/tests/image/test-image.sh '$(TEST_IMAGE_REPOSITORY):$(shell git rev-parse HEAD)'

# An operator pushes the test image to ECR and moves a consumer's pin
# under their own AWS credentials (ADR-0093), so neither target is in
# `ci`. test-image-push checks HEAD as test-image-e2e does and pushes it
# under its first twelve hex digits (ADR-0071); test-image-pin moves
# `pinned-$(CONSUMER)` to the image of $(COMMIT). Both scripts hold the
# repository, so no make variable can redirect them.
test-image-push:
	tooling/image-test-push.sh

test-image-pin:
	tooling/image-test-pin.sh '$(COMMIT)' '$(CONSUMER)'
