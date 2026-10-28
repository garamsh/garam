# Local Kubernetes Stack

Runs every garam process on a Kubernetes cluster on this machine, and
nowhere else.

This is a development artefact. garam publishes images and a contract,
not manifests for a cluster it does not run
([ADR-0020](../../../docs/architecture/adr/0020-published-artifacts.md)),
so nothing here is a starting point for one: it has no ingress, no DNS,
no certificate any authority issued, and its images exist only in this
cluster's own daemon.

## Contents
- What runs
- Up and down
- Reaching it
- Key material
- Images
- Which cluster, and whose kubeconfig
- Startup order
- Database credentials
- What this proves and what it does not

## What runs

- `postgres` — one instance, reconciled by the CloudNativePG operator.
- `migrate` — a Job that applies pending migrations and exits.
- `authz`, `api`, `machine` — the three serve processes of
  [ADR-0017](../../../docs/architecture/adr/0017-machine-process.md),
  from one image.
- `web` — the Next.js console.

All of it lives in the `garam-local` namespace. Manifests are
`namespace.yaml`, `postgres.yaml`, `migrate-job.yaml` and `garam.yaml`
in this directory; they are separate files because the bring-up applies
them in that order.

The operator comes from `CNPG_MANIFEST` in `deploy/local/Makefile`, pinned to
the commit that carries the manifest: a release asset can be replaced
under its tag and a commit cannot. v1.30.0 is the first release patched
for all four advisories github.com/cloudnative-pg/cloudnative-pg
publishes. GHSA-7qwx-x8ff-3px9 is what puts that floor at 1.30.0 rather
than 1.29.2, and the GitHub Advisory Database does not carry it, so the
four are re-checked at the project's own advisories. The database's
Postgres major is not this pin's to move: `postgres.yaml` names its own
image rather than taking the default this operator version carries.

## Up and down

```sh
make -C deploy/local minikube-up        # cluster, operator, database, secret, images, migrations, processes
make -C deploy/local minikube-rebuild   # after a code change: images, migrations, processes
make -C deploy/local minikube-forward   # publishes the web app on localhost; holds the terminal
make -C deploy/local minikube-down      # removes the namespace, its volume and its key material
```

`make -C deploy/local minikube-up` starts the cluster if it is not
already running, so it works from nothing. Rerunning it is safe: the key
material is kept rather than regenerated, and the migration Job runs
again against an already-migrated database and applies nothing.

`make -C deploy/local minikube-rebuild` is what a code change needs, and
it is the whole of `minikube-up` past the cluster and the database. The
two share one list of steps rather than two, so a rebuild cannot come
out a step short of what a bring-up runs — a change carrying a migration
has that migration applied before the processes that read the schema
start.

Each step is still its own target — `minikube-start`, `minikube-images`,
`minikube-db`, `minikube-secret`, `minikube-migrate`, `minikube-apps` —
for when one of them alone is what you want.
`make -C deploy/local minikube-images minikube-apps` is not the rebuild:
it puts the new binary in front of whatever schema the database already
has.

## Reaching it

Nothing here is published to the host. The Services are cluster-internal
and there is no ingress, so `make -C deploy/local minikube-forward` is
the only way in: it forwards the web app to `http://localhost:3000`.

`/login` takes Google, which needs real credentials this deployment
sets none of, or a username and a password, which `/register` mints
only from an invitation an org admin issued
([ADR-0041](../../../docs/architecture/adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)).
The database this stack starts with holds no admin, so reaching the
console means writing user and session rows against it directly, as
`console/e2e/seed.ts` does for the browser suite.

To reach anything else — the api, the authz engine's metrics, the
machine process's probes — forward its Service the same way.

## Key material

`serve machine` needs a certificate and its key, the garam server root
that signed them, and a key-encryption key, and there is no cert-manager
and no key store here to get any of them from.
`machine-key-material.sh` generates them and puts them in a Secret. All
of it is disposable:

- The root is self-signed, valid for a year, and trusted by nothing off
  this machine. A real deployment holds this authority's key where garam
  cannot reach it
  ([ADR-0040](../../../docs/architecture/adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md));
  here it is thrown away with the temporary directory, so nothing
  renews.
- The certificate that root signs is valid for a month.
- The key-encryption key seals message signing keys that exist only in
  this cluster's database.

**A real key-encryption key is not like this.** A cluster that loses one
loses every key sealed under it, with no recovery
([ADR-0016](../../../docs/architecture/adr/0016-ca-key-custody.md)). Do
not carry anything from this directory into a deployment that holds one.

The script generates into a private temporary directory and deletes it
on the way out, so no generated value is ever written into this
repository. It never replaces a key-encryption key that already exists:
a new one would leave every key already sealed in the database
unopenable. A Secret that already carries a server root is left alone
whole; one predating the root gets fresh TLS material beside the key
that was already there, so a cluster brought up before
[ADR-0040](../../../docs/architecture/adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
keeps its authorities and starts.

Key material reaches the process as a file readable only by its owner,
with the process running as that owner
([ADR-0015](../../../docs/architecture/adr/0015-operator-identity.md)).
A Secret volume is owned by root, so an init container copies the files
as the same unprivileged user the process runs as, into a memory-backed
volume that never reaches the node's disk. The serving container does
not mount the Secret at all.

## Images

Both images are built into the cluster's own daemon and pushed nowhere.
No node here holds a registry credential, and a token for the cluster
this project targets expires within the day (ADR-0020). The manifests
set `imagePullPolicy: Never`, so a missing image fails as the build it
is rather than as a silent pull from somewhere else.

They are built from `deploy/local/Dockerfile` and
`deploy/local/Dockerfile.web` — the same two the compose stack uses.

Each pod template carries the id of the image it runs, stamped in by
`make -C deploy/local minikube-apps`. The tag alone cannot carry a
rebuild: `garam:local` names a different image after every build and the
same Deployment before and after it, which leaves `kubectl apply` a
no-op, creates no ReplicaSet, and has `rollout status` report the pods
still on the previous binary as fully rolled out. With the id in the
template a rebuilt image is a changed Deployment, and an unrebuilt one
restarts nothing.

## Which cluster, and whose kubeconfig

Every command names `--context minikube`, so nothing in `deploy/local/Makefile`
can reach a cluster that is not the local one.

The cluster's credentials go in `$HOME/.minikube/kubeconfig`, beside the
rest of its state, rather than into `~/.kube/config` — bringing up a
local cluster should not write into the file that holds credentials for
a real one. `kubectl` outside `make` therefore needs to be told:

```sh
export KUBECONFIG=$HOME/.minikube/kubeconfig
```

Set `KUBECONFIG` before `make -C deploy/local minikube-up` to put the
context somewhere else, your own kubeconfig included.

A start run outside `make` — a reboot followed by a plain
`minikube start` is enough — gives the apiserver a new port and writes
it into `~/.kube/config`, leaving the file above pointing at a port
that no longer answers. Every target that reaches the cluster checks
that this file answers before acting and refreshes it when it does
not, so the next `make` run repairs the state on its own. The one
exception is `minikube-down`, which never boots a cluster to delete
from one: after a restart outside `make`, run any other target first.

## Startup order

The bring-up sequences what a manifest cannot express: Postgres has to
serve before the migration runs, and the migration has to finish before
anything that reads the schema starts.
`make -C deploy/local minikube-migrate` waits for the Job to complete,
and `make -C deploy/local minikube-apps` runs after it.

`make -C deploy/local minikube-db` waits twice. The operator reports the
cluster ready before the Service in front of it carries a routable
endpoint (CloudNativePG 1.30.0, the release `CNPG_MANIFEST` applies), so
waiting on the cluster alone leaves the first migration attempt to fail
with `connection refused`; the second wait is on the endpoint the
migration dials.

A Job's pod template is immutable (read 2026-08-17; this repository
pins no Kubernetes version), so the finished Job is deleted before
being applied again.

Once running, each process reports for itself. `/healthz` answers for
the process alone and `/readyz` for what it depends on, on the ports
[ADR-0021](../../../docs/architecture/adr/0021-liveness-and-readiness.md)
publishes. A database outage takes the pods out of rotation and leaves
them running, so they serve again when it returns.

The web app answers the same two paths
([ADR-0023](../../../docs/architecture/adr/0023-web-probes.md)), and
neither of them reads the api, so an api outage leaves its pods in
rotation. They replaced probes on two page routes: readiness asked for
`/login`, which the app can only render by reaching the api, and
liveness had already been split off it because probing it restarted the
web app every time the api was down — the amplification ADR-0021 exists
to prevent.

## Database credentials

The operator generates the database role's password and publishes it,
with a ready-made connection URI, in a Secret it maintains
(CloudNativePG 1.30.0). Every process reads `GARAM_DATABASE_URL` from
that Secret, so no file under `deploy/local/minikube/` holds a database
credential.

## What this proves and what it does not

It proves the processes survive being scheduled, probed, restarted, and
given their configuration the way a cluster gives it.

It does not prove anything about the registry path, which this stack
deliberately does not use, and bringing the stack up does not exercise
the machine process's surface: that listener admits no caller without
a token, and none is exchanged as part of the bring-up. What is
verified there is that the process starts with its key material and
that it answers its own probe.

Reaching that surface by hand starts in the console: register an
operator and issue it a key, then exchange an assertion that key signs
at the machine listener's `/oauth/token` for a token (ADR-0102,
ADR-0117), with the `machine` Service port-forwarded for the calls.
