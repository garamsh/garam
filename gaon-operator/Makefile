# gaon-operator — the Go module behind the gaon operator (ADR-0151).
# Its verbs are every module's (#1661); `make -C gaon-operator ci` is
# its gate. Nothing in `ci` needs Docker.

include ../tooling/make/go.mk

.PHONY: ci format lint test build guest-image guest-run pair-run pair-run-gaon

ci: format lint test build

format:
	$(go-format)

# A file only gaon's module builds, as pair-run/testdata/gaonharness
# is, belongs under testdata: go mod tidy reads every other package.
lint:
	$(go-vet)
	$(go-tidy-check)

test:
	$(go-test)

build:
	$(go-build)

# guest-image and guest-run are not part of `ci`: they need a Docker
# daemon, a login to the registry holding the three pinned images, and
# openssl. guest-run starts the composed guest image as one container
# and checks the supervisor's start order, its readiness port and its
# SIGTERM drain (guest/run.sh).
GUEST_IMAGE ?= gaon-guest:local

# The image is the one the release builds, from the same parse of
# release.yml (ADR-0156), so a change to how the release builds the
# guest is what guest-run runs.
GUEST_REPOSITORY = garam/gaon-guest

guest-image:
	cd .. && tooling/image-build.sh --only $(GUEST_REPOSITORY)
	docker tag $(GUEST_REPOSITORY):ci $(GUEST_IMAGE)

guest-run: guest-image
	sh guest/run.sh $(GUEST_IMAGE)

# pair-run is not part of `ci` either: it needs a Docker daemon, a login
# to the registry holding the control service's image, openssl, jq,
# curl and the Go toolchain. It runs this operator against garam's stack
# and the control service, with tests/gaonstandin for gaon's API, and
# exits non-zero where an answer is not the one it names
# (pair-run/pair-run.sh). Name the compose project with
# PAIR_RUN_PROJECT.
pair-run:
	sh pair-run/pair-run.sh

# pair-run-gaon is the same run against gaon's own built services in
# place of the stand-in, with authorization on: an embedded etcd,
# gaon-apiserver (RBAC), gaon-controllers, gaon-scheduler and a Node
# Agent over a minimal mvmd. GAON_REPO names a gaon checkout, which is
# cloned and not changed, and GAON_COMMIT the commit built from it. It
# needs the Go module cache to hold gaon's etcd and mvmd's runtime API
# (pair-run/gaon-build.sh), and is not part of `ci`.
# The PM moves GAON_COMMIT with the lib/api pin in go.mod, to the same
# commit of gaon, in the same pull request.
GAON_COMMIT ?= 2387cebd0b44b322b7da2ff485144a7456a97c6f

pair-run-gaon:
	test -n "$(GAON_REPO)" || { echo "pair-run-gaon: GAON_REPO names the gaon checkout to clone" >&2; exit 1; }
	PAIR_RUN_GAON=gaon GAON_REPO="$(GAON_REPO)" GAON_COMMIT="$(GAON_COMMIT)" sh pair-run/pair-run.sh
