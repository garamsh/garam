# Where the local stack deploy/local/Makefile brings up is published on
# this host, shared by every module whose suites run against it (#1661),
# so each address and database name is written once.

STACK_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/../..)

# One test database per worktree, on the one dev Postgres every
# worktree shares (compose pins `name: garam-local`). The suffix is
# git's name for a linked worktree and empty in the main checkout,
# which keeps garam_test. Both Go suites migrate it and the integration
# suite truncates every table, so no worktree shares another's.
TEST_DB_NAME ?= garam_test$(shell git rev-parse --git-dir | sed -n 's|.*/worktrees/|_|p')
export TEST_DB_NAME
# cowork owns its database (ADR-0125), so its suite has one of its own
# on the same server, named on the same terms.
COWORK_TEST_DB_NAME ?= cowork_test$(shell git rev-parse --git-dir | sed -n 's|.*/worktrees/|_|p')

# IMAGE_BUILD_CACHE=gha has `backend-up` and `image-build` read and
# write their layers in the GitHub Actions cache, which only a CI run
# holds (#1519). Empty, the default, builds as before.
IMAGE_BUILD_CACHE ?=
ifneq ($(filter-out gha,$(IMAGE_BUILD_CACHE)),)
$(error IMAGE_BUILD_CACHE is '$(IMAGE_BUILD_CACHE)', and gha is the one cache it can name)
endif

COMPOSE_LOCAL := docker compose -f $(STACK_ROOT)/deploy/local/docker-compose.yml$(if $(IMAGE_BUILD_CACHE), -f $(STACK_ROOT)/deploy/local/docker-compose.gha-cache.yml)
export COMPOSE_LOCAL

# Where the api and the dev Postgres are published on this host, asked
# of compose at the moment each is used. GARAM_LOCAL_API_PORT and
# GARAM_DB_PORT move them, from deploy/local/.env as readily as from the
# environment, and a second copy of either here would be the copy that
# goes stale. Each is empty when nothing is published, which the suites
# report rather than dial.
API_HOST_ADDR = $(shell $(COMPOSE_LOCAL) port api 8080 2>/dev/null | sed 's/^0\.0\.0\.0/localhost/')
DB_HOST_ADDR = $(shell $(COMPOSE_LOCAL) port postgres 5432 2>/dev/null | sed 's/^0\.0\.0\.0/localhost/')
# The dev OpenBao `integration` signs against (ADR-0104), rewritten to
# localhost because its TLS certificate names that; GARAM_OPENBAO_PORT
# moves it. The container is named so the suite can read the CA and
# root token the dev server generated.
OPENBAO_HOST_ADDR = $(shell $(COMPOSE_LOCAL) port openbao 8200 2>/dev/null | sed 's/^0\.0\.0\.0/localhost/')
OPENBAO_CONTAINER = $(shell $(COMPOSE_LOCAL) ps -q openbao 2>/dev/null)
# The RustFS cowork's blob store is tested against, and the keys
# deploy/local/docker-compose.yml gives it.
COWORK_S3_HOST_ADDR = $(shell $(COMPOSE_LOCAL) port rustfs 9000 2>/dev/null | sed 's/^0\.0\.0\.0/localhost/')
COWORK_S3_ACCESS_KEY := cowork
COWORK_S3_SECRET_KEY := cowork-local-secret

# Empty rather than half a URL when no Postgres is published, so the
# suites have one thing to test. Set it in the environment to run them
# against a database outside compose.
TEST_DATABASE_URL ?= $(if $(DB_HOST_ADDR),postgres://garam:garam@$(DB_HOST_ADDR)/$(TEST_DB_NAME)?sslmode=disable)
COWORK_TEST_DATABASE_URL ?= $(if $(DB_HOST_ADDR),postgres://garam:garam@$(DB_HOST_ADDR)/$(COWORK_TEST_DB_NAME)?sslmode=disable)

# The Playwright suites seed sessions for the api they drive, so this
# URL names the stack's own `garam` rather than a database of its own.
# Empty and overridable on the same terms as the line above.
WEB_E2E_DATABASE_URL ?= $(if $(DB_HOST_ADDR),postgres://garam:garam@$(DB_HOST_ADDR)/garam?sslmode=disable)
