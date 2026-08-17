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

## Up and down

```sh
make minikube-up        # cluster, operator, images, database, secret, migrations, processes
make minikube-forward   # publishes the web app on localhost; holds the terminal
make minikube-down      # removes the namespace, its volume and its key material
```

`make minikube-up` starts the cluster if it is not already running, so
it works from nothing. Rerunning it is safe: the key material is kept
rather than regenerated, and the migration Job runs again against an
already-migrated database and applies nothing.

Each step is its own target — `minikube-start`, `minikube-images`,
`minikube-db`, `minikube-secret`, `minikube-migrate`, `minikube-apps` —
so rebuilding after a code change is `make minikube-images
minikube-apps` rather than the whole sequence.

## Reaching it

Nothing here is published to the host. The Services are cluster-internal
and there is no ingress, so `make minikube-forward` is the only way in:
it forwards the web app to `http://localhost:3000`, which is the port
the api's dev callback is configured for. Move one and the sign-in flow
breaks.

`GARAM_DEV_AUTH` is on, so `/login` offers a one-click sign-in that
needs no Google credentials
([ADR-0010](../../../docs/architecture/adr/0010-dev-auth-provider.md)).
That is why this stack must not be reachable from anywhere but this
machine.

To reach anything else — the api, the authz engine's metrics, the
machine process's probes — forward its Service the same way.

## Key material

`serve machine` needs a server certificate and a key-encryption key, and
there is no cert-manager and no key store here to get them from.
`machine-key-material.sh` generates both on the first bring-up and puts
them in a Secret. Both are disposable:

- The certificate is self-signed, valid for a month, and trusted by
  nothing.
- The key-encryption key seals certificate authorities that exist only
  in this cluster's database.

**A real key-encryption key is not like this.** A cluster that loses one
loses every organization's certificate authority, with no recovery
beyond reissuing every certificate from new authorities
([ADR-0016](../../../docs/architecture/adr/0016-ca-key-custody.md)). Do
not carry anything from this directory into a deployment that holds one.

The script generates into a private temporary directory and deletes it
on the way out, so no generated value is ever written into this
repository. It keeps an existing Secret rather than replacing it: a new
key-encryption key would leave every authority already in the database
unopenable.

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

## Which cluster, and whose kubeconfig

Every command names `--context minikube`, so nothing in the `Makefile`
can reach a cluster that is not the local one.

The cluster's credentials go in `$HOME/.minikube/kubeconfig`, beside the
rest of its state, rather than into `~/.kube/config` — bringing up a
local cluster should not write into the file that holds credentials for
a real one. `kubectl` outside `make` therefore needs to be told:

```sh
export KUBECONFIG=$HOME/.minikube/kubeconfig
```

Set `KUBECONFIG` before `make minikube-up` to put the context somewhere
else, your own kubeconfig included.

## Startup order

The bring-up sequences what a manifest cannot express: Postgres has to
serve before the migration runs, and the migration has to finish before
anything that reads the schema starts. `make minikube-migrate` waits for
the Job to complete, and `make minikube-apps` runs after it.

`make minikube-db` waits twice. The operator reports the cluster ready
before the Service in front of it carries a routable endpoint, so
waiting on the cluster alone leaves the first migration attempt to fail
with `connection refused`; the second wait is on the endpoint the
migration dials.

A Job's pod template is immutable, so the finished Job is deleted before
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
with a ready-made connection URI, in a Secret it maintains. Every
process reads `GARAM_DATABASE_URL` from that Secret, so no database
credential is written in this repository.

## What this proves and what it does not

It proves the processes survive being scheduled, probed, restarted, and
given their configuration the way a cluster gives it.

It does not prove anything about the registry path, which this stack
deliberately does not use, and it does not exercise the machine
process's mTLS surface: that listener admits no caller without a client
certificate, and issuing one is not part of bringing the stack up. What
is verified there is that the process starts with its key material, that
it answers its own probe, and that the mTLS listener refuses a caller
presenting no certificate.
