# gaon-operator — the Go module behind the gaon operator (ADR-0151).
# Its verbs are every module's (#1661); `make -C gaon-operator ci` is
# its gate. Nothing in `ci` needs Docker.

include ../tooling/make/go.mk

.PHONY: ci format lint test build guest-image guest-run pair-run

ci: format lint test build

format:
	$(go-format)

lint:
	$(go-vet)

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
