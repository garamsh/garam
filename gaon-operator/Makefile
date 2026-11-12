# gaon-operator — the Go module behind the gaon operator (ADR-0151).
# Its verbs are every module's (#1661); `make -C gaon-operator ci` is
# its gate. Nothing in `ci` needs Docker.

include ../tooling/make/go.mk

.PHONY: ci format lint test build guest-image guest-run

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

guest-image:
	docker build -f guest/Dockerfile -t $(GUEST_IMAGE) \
		--build-arg REVISION=$(shell git rev-parse HEAD) ..

guest-run: guest-image
	sh guest/run.sh $(GUEST_IMAGE)
