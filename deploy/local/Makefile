# deploy/local — the stack the suites run against, in compose, and the
# whole stack on a local Kubernetes cluster. It has no gate of its own:
# a module whose suites need the stack names the verb here its
# `stack-up` runs (#1661), and its suites are what check this folder.

include ../../tooling/make/stack.mk

.PHONY: db-up db-down db-prune backend-up
.PHONY: minikube-up minikube-down minikube-start minikube-images minikube-db
.PHONY: minikube-secret minikube-migrate minikube-apps minikube-rebuild minikube-forward minikube-context

# db-up starts postgres, the openbao `integration` signs against and the
# rustfs cowork's blob store is tested against, and
# creates the two test databases, the platform's and cowork's, beside
# `garam` where absent: Postgres has no
# CREATE DATABASE IF NOT EXISTS, so it asks psql first. Output and exit
# status are read apart, because one non-zero status cannot tell a
# failed query from a clean negative (#344).
db-up:
	$(COMPOSE_LOCAL) up -d --wait postgres openbao rustfs
	@for db in $(TEST_DB_NAME) $(COWORK_TEST_DB_NAME); do \
		present="$$($(COMPOSE_LOCAL) exec -T postgres psql -U garam -d postgres -tAc \
			"SELECT 1 FROM pg_database WHERE datname='$$db'")" \
			|| { echo "db-up: could not ask postgres whether $$db exists" >&2; exit 1; }; \
		test -n "$$present" || $(COMPOSE_LOCAL) exec -T postgres createdb -U garam $$db || exit 1; \
	done

db-down:
	$(COMPOSE_LOCAL) rm -sf postgres openbao rustfs

# db-prune drops the per-worktree databases db-up created for worktrees
# that are gone. It is in no whole-set name and no workflow: a runner
# starts empty, and the target is destructive on a developer machine.
# The script holds the keep set and the controls it rests on (#943).
db-prune:
	@../../tooling/db-prune.sh

# backend-up starts what the browser suites drive — postgres, the
# one-shot migration, authz and api — and returns once api answers; the
# profile-gated `web` service stays down. It builds first, migrations
# included, because compose starts an existing image stale and reports
# it healthy (#297).
backend-up:
	$(COMPOSE_LOCAL) up -d --build --wait api

# ---------------------------------------------------------------------
# deploy/local/minikube — the whole stack on a local Kubernetes cluster.
# Development only: every image is built into that cluster's own daemon
# and nothing is pushed or pulled, because no node here holds a registry
# credential (ADR-0020). What reconciles a real cluster is not this.
# ---------------------------------------------------------------------

# The local cluster keeps its credentials beside its own state instead
# of in ~/.kube/config, so bringing it up never writes into a kubeconfig
# holding credentials for a real cluster. An exported KUBECONFIG wins,
# so `KUBECONFIG=~/.kube/config make -C deploy/local minikube-up` merges
# the context into your own.
KUBECONFIG ?= $(HOME)/.minikube/kubeconfig
export KUBECONFIG

# Every command names the context, so nothing in this file can reach a
# cluster that is not the local one, whichever kubeconfig it reads.
KUBECTL_MINIKUBE := kubectl --context minikube
KUBECTL_LOCAL := $(KUBECTL_MINIKUBE) --namespace garam-local
MINIKUBE_DIR := minikube

# The CloudNativePG operator, pinned to the commit carrying the
# manifest; deploy/local/minikube/README.md §What runs records why
# v1.30.0 and where its advisories are re-checked. Nothing proposes a
# bump, so it is the PM's to move (AGENTS.md §Roles).
CNPG_MANIFEST := https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/4b5e244a7d031f67e025c83c1555e7726ecbbfa1/releases/cnpg-1.30.0.yaml

minikube-up: minikube-start minikube-db minikube-secret minikube-rebuild

# minikube-rebuild is everything a code change touches: the images, the
# migrations the new code expects, and the processes that run it. The
# bring-up above composes it rather than restating its steps, so the two
# cannot come apart.
minikube-rebuild: minikube-images minikube-migrate minikube-apps

# Every kubectl recipe needs the apiserver reachable through the project
# kubeconfig, and `minikube status` does not answer that: a cluster
# started outside make writes the new apiserver port into ~/.kube/config
# and leaves this file stale behind a Running process, which status's
# exit code also reports as failure. Ask the file itself and repair it
# with update-context; boot only when the apiserver process is down.
minikube-context:
	@$(KUBECTL_MINIKUBE) get --raw=/healthz >/dev/null 2>&1 || { \
		test "$$(minikube status --format '{{.APIServer}}' 2>/dev/null | head -n 1)" = Running \
			|| minikube start --driver=docker; \
		minikube update-context; \
	}

# minikube-start brings up the cluster and the Postgres operator, and is
# a no-op on both when they are already there. The CRDs are past the
# annotation size a client-side apply writes, so the apply is
# server-side.
minikube-start: minikube-context
	$(KUBECTL_MINIKUBE) apply --server-side -f $(CNPG_MANIFEST)
	$(KUBECTL_MINIKUBE) -n cnpg-system rollout status deployment/cnpg-controller-manager --timeout=300s

# minikube-images builds both images (ADR-0020) into the cluster's own
# daemon, which docker-env points the client at for the length of the
# recipe. Nothing is pushed, and the manifests pull Never, so what runs
# is what was built here. Both Dockerfiles sit outside the context they
# build — `minikube image build` uploads the context and cannot reach
# them — and this is the same pair of contexts the compose file uses.
minikube-images:
	eval "$$(minikube docker-env --shell bash)"; \
		docker build -t garam:local -f Dockerfile ../.. && \
		docker build -t garam-web:local -f Dockerfile.web ../../console

# minikube-db creates the namespace everything else lands in and the
# Postgres cluster CNPG reconciles, and returns once it is reachable.
# The operator reports the cluster ready before its Service carries a
# routable endpoint, and everything downstream connects through that
# Service — so the second wait is on the endpoint the migration
# actually dials, not on the condition next to it.
minikube-db: minikube-context
	$(KUBECTL_MINIKUBE) apply -f $(MINIKUBE_DIR)/namespace.yaml
	$(KUBECTL_MINIKUBE) apply -f $(MINIKUBE_DIR)/postgres.yaml
	$(KUBECTL_LOCAL) wait --for=condition=Ready cluster/postgres --timeout=300s
	$(KUBECTL_LOCAL) wait --for=jsonpath='{.endpoints[0].conditions.ready}'=true endpointslice -l kubernetes.io/service-name=postgres-rw --timeout=120s

minikube-secret: minikube-context
	$(MINIKUBE_DIR)/machine-key-material.sh

# minikube-migrate applies pending migrations and waits for them. A
# Job's pod template is immutable, so the finished one is deleted rather
# than updated.
minikube-migrate: minikube-context
	$(KUBECTL_LOCAL) delete job migrate --ignore-not-found
	$(KUBECTL_LOCAL) apply -f $(MINIKUBE_DIR)/migrate-job.yaml
	$(KUBECTL_LOCAL) wait --for=condition=complete job/migrate --timeout=300s

# minikube-apps starts the three serve processes and the web app, and
# reports what is running rather than that it finished. Each pod
# template is stamped with the id of the image it runs, so a rebuilt
# image is a changed Deployment the apply rolls and an unrebuilt one
# restarts nothing — which a mutable tag alone cannot tell apart.
minikube-apps: minikube-context
	@eval "$$(minikube docker-env --shell bash)"; \
		id="$$(docker image inspect --format '{{.Id}}' garam:local)" && \
		web_id="$$(docker image inspect --format '{{.Id}}' garam-web:local)" && \
		echo "minikube-apps: garam:local=$$id garam-web:local=$$web_id" && \
		sed -e "s|GARAM_IMAGE_ID|$$id|" -e "s|GARAM_WEB_IMAGE_ID|$$web_id|" $(MINIKUBE_DIR)/garam.yaml \
		| $(KUBECTL_LOCAL) apply -f -
	@for d in authz api machine web; do $(KUBECTL_LOCAL) rollout status deployment/$$d --timeout=300s; done
	@$(KUBECTL_LOCAL) get pods

# minikube-forward is how the web app is reached: the Services are
# cluster-internal and there is no ingress, so nothing leaves the
# machine without this. It holds the terminal until interrupted.
minikube-forward: minikube-context
	$(KUBECTL_LOCAL) port-forward service/web 3000:3000

# minikube-down removes the stack, its database volume and its key
# material, and leaves the cluster and the operator up.
minikube-down:
	$(KUBECTL_MINIKUBE) delete -f $(MINIKUBE_DIR)/namespace.yaml --ignore-not-found
