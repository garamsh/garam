# What a gaon deployment of garam declares

How each garam process in
[deployment-needs.md](./deployment-needs.md) is declared on a gaon
cluster, as the gaon kind that holds it and what that object states, so
that aurora can write garam's objects from one document.

## Contents

- What it is
- How a claim about gaon is read
- The kind of each process
- What each object states
- Order
- Names
- Databases and object storage
- The Transit arrangement
- What gaon does not yet serve
- Not established

## What it is

This document says nothing about what garam needs; that is
[deployment-needs.md](./deployment-needs.md), and every setting, file,
listener and probe below is routed to its entry there and is not
restated. It states only what gaon adds: which kind declares a process,
what the object says, in what order the objects are created, and which
objects serve a name.

Every claim about garam holds at commit
`faf084f3593b36777b5eeec7177336266976f27b` (`origin/dev` when this was
written), read from the files cited. Every claim about gaon holds at
commit `c7e8436d85cd6e49e57ba2c39a75221edc6d8a4e` of `garamsh/gaon`
(its `main` when this was written), read from the file or issue cited,
and is not checked against any later one. Gaon's API group is
`compute.gaon.garam.sh/v1alpha1` and its workload kinds are in
`workloads.gaon.garam.sh/v1alpha1`; every object is cluster-scoped, as
gaon has no namespaces
(`docs/architecture/resource-api.md`).

The sections §How a claim about gaon is read, §Order, §Names and
§What gaon does not yet serve were revised for
[ADR-0155](./adr/0155-garam-ships-a-second-helm-chart-that-renders-the-gaon-kinds.md)
and hold for gaon at commit
`cfb148dea568674525207bd329a94f1d9ccc912f` (its `main` when they were
revised), read from the file or issue cited. The other sections keep
the commit above.

The chart in
[deploy/charts/garam-gaon/](../../deploy/charts/garam-gaon/README.md)
renders the objects of this document, so that aurora supplies values
and writes no template of garam's. Nothing applies that chart to a gaon
cluster: what is shown is that it renders, with the pinned Helm, the
fields below and no others, and the cluster-facing claim is not shown.

The gaon operator
([ADR-0151](./adr/0151-the-gaon-operator-is-a-service-here-that-runs-each-managed-agent-as-one-microvm.md))
is out of scope. A release publishes its image `garam/gaon-operator`
and the guest's `garam/gaon-guest`
([ADR-0156](./adr/0156-a-release-publishes-eight-images-and-a-pull-request-builds-the-guest-only-to-its-build-stage.md)),
and it is deployed as a workload of its own; nothing here depends on it.

## How a claim about gaon is read

Gaon's documents describe two things that differ, and a line below says
which one it rests on.

- **Contract.** What a gaon document states a kind or field to be. A
  document may describe a field before the API server serves it.
- **Served.** What the API server of `main` accepts. Only the claims
  marked served below rest on gaon's code or issues, and each names the
  issue or document that says so.

These cases decide most of this document:

- The Deployment, Job, CronJob and StatefulSet kinds, the MicroVM's
  `env`, `files`, `ports`, probes and `terminationGracePeriodSeconds`,
  the Secret, ConfigMap and ServiceAccount kinds and the ServiceAccount
  token file are served: `docs/architecture/workloads.md`,
  `resource-api.md` and `node-agent.md` describe them as built, and
  ADR 0020, 0024, 0026, 0029 and 0030 decide them
  (`docs/architecture/adr/` of gaon at the same commit).
- `Service`, `Endpoints` and `NetworkPolicy` are served at
  `cfb148dea568674525207bd329a94f1d9ccc912f`, by the commits that
  gaon's log names: the kinds and Service addresses by
  `02eecea16a2ef114b3dfbf77010c5286caa19026` (#190), a selector
  Service's endpoints by
  `fb9ea18eaa9a2788da8802092fa610e5a21093f7` (#202), enforcement by
  the network plugin by `99a1f863e74b524ef0db70c2d0ccd6980ca77b20`
  (#204) and the CLI's reach of them by
  `0496411c9ec2d37010ae6dc7d353a99f10b0266d` (#208). garamsh/gaon#182
  and #165 are closed. This replaces the earlier reading of these kinds
  as contract only, which held at
  `c7e8436d85cd6e49e57ba2c39a75221edc6d8a4e`.
- `gaon apply -f -` applies a Helm-rendered stream, by
  `e80efe9b3ec7058735831dc451a13d088e05af79` (#206) and `cli.md`
  §Applying a Helm-rendered stream: it keeps Helm's order, and offers
  no hook, no prune and no history.
- The gateway is contract only. `gateway.md`, from
  `70abdf8a0909b175261090e5ff4851a9ab275f72` (#200), states its kinds
  and ADR 0032 decides them; gaon has no unit that serves them at the
  commit above.
- DNS for a `Service` is contract (`dns.md`, from
  `215c7949564c1b55360b9fe0de50a15c2d7cae8f`, #203) and a `dns/` unit,
  `gaon-dns`, exists (`cfb148dea568674525207bd329a94f1d9ccc912f`,
  #225). `dns.md` says a MicroVM resolves nothing until the network
  plugin implements Service addresses, garamsh/gaon#196, which was open
  at that commit. That a MicroVM's resolver reaches the unit is not
  shown.

A field name appears below only where a gaon document shows it. Where
none does, the gap table says so.

## The kind of each process

- **A replicated workload, `Deployment`** for a process that serves and
  has no end: `serve api`, `serve authz`, `serve machine`,
  `cowork serve`, `policy serve`, `garam/console`, `garam/site` and
  `garam/cowork-web`.
- **A run-to-completion workload, `Job`** for a process that applies
  something and exits: `garam migrate`, `cowork migrate` and
  `policy migrate`.
- **A bare `MicroVM`** for none. A `Job` carries a `Complete` or
  `Failed` condition the deployer reads, and a bare MicroVM with
  `runStrategy: Once` carries only its own `phase`
  (`workloads.md`, `resource-api.md` §MicroVM); a `Deployment` is the
  kind that holds a MicroVM at the replica count.
- **`CronJob` and `StatefulSet`** for none. No process here runs on a
  schedule, and every process keeps its state in PostgreSQL or S3
  ([Databases and object storage](#databases-and-object-storage)).

Each of the eight `Deployment`s is declared with `replicas: 1`.
[deployment-needs.md](./deployment-needs.md) §Not established puts
replica safety above one outside what is established for every process
here, and [deployment-contract.md](./deployment-contract.md)
§Not established does so for the three `serve` commands of
`garam/server`, so no object states more than one. Because a surge is a
second replica, each uses the `Recreate` strategy and not
`RollingUpdate` (`workloads.md`, which gives both): an update stops the
old MicroVM and then starts the new, and the process is down between
them. ADR 0026 decides that a replica is available as soon as it is
Ready, with no minimum ready time, and that there is no ReplicaSet
kind, so a rolling update would also give no hold on how long the new
replica is ready before the old is removed. `Recreate` is, in that ADR,
a surge of 0 and an unavailability of `replicas`.

Spread across Nodes follows from the replica count and states nothing:
with one replica there is nothing to spread. Gaon's decision that a
replicated kind spreads across Nodes through a topology-spread filter
plugin is a comment on garamsh/gaon#117, which numbers it D16
(`https://github.com/garamsh/gaon/issues/117#issuecomment-6830309068`);
the issue's body numbers a different decision D16. It is not in
`scheduler.md`, which says it has no spreading.

## What each object states

A table of what the objects of one kind say in common, then one row
per process for what differs.

**Common to every object**

| Field | What it states | Basis |
|---|---|---|
| `image` | `<repository>@sha256:<digest>` of the release; a tag is refused | `resource-api.md` §MicroVM |
| `command`, `args` | the image's entry point and the garam command; a `$(VAR)` in either is not expanded | `resource-api.md` §MicroVM |
| `user` | `uid:gid`, both numbers: the uid from [deployment-needs.md](./deployment-needs.md) §The images, `10001` for `garam/server`, `garam/cowork` and `garam/policy` and `1000` for the three web images, and the gid of that account in the image | `node-agent.md`, ADR 0029 |
| `env` | one entry per setting: `value` for a setting that is no secret, `valueFrom.secretKeyRef` or `valueFrom.configMapKeyRef` of `{name, key}` otherwise | `resource-api.md` §MicroVM |
| `files` | one entry per file, below | `resource-api.md` §MicroVM |
| `ports` | one named port per listener, a name of at most 15 characters | `resource-api.md` §MicroVM |
| `resources` | `requests` of `cpu` and `memory` for every object, from a value with no default; `limits` where the deployment sets them. A MicroVM that names no cpu or no memory is `InvalidSpec`, so the field is not optional; the figure is [Not established](#not-established) | `resource-api.md` §MicroVM (line "resources (requests and limits for cpu and memory)"), `lib/api/microvm.go` |
| `terminationGracePeriodSeconds` | gaon's default of 30 | `resource-api.md` §MicroVM |
| `runStrategy` | `Always` under a `Deployment`; a `Job` forces `Once` | `workloads.md` |

- **Why `user` is `uid:gid`.** A file's owner is its own `owner`, else
  the MicroVM's `user` when that is a numeric `uid:gid`, else root when
  there is no `user`; a file that gives no owner under any other `user`,
  a bare name included, fails the MicroVM as `InvalidSpec`
  (`node-agent.md`). The scratch images carry a passwd file naming the
  account, but the numeric form does not depend on it. A bare number is
  not stated there to be accepted, so the form is `uid:gid`; the gid is
  under §Not established.
- **Which settings come from a `Secret`.** Exactly the ones
  [deployment-needs.md](./deployment-needs.md) §Secrets and key
  material lists with kind "environment" or "a URL that may carry a
  password": the three database URLs, `GOOGLE_CLIENT_SECRET`,
  `COWORK_S3_ACCESS_KEY`, `COWORK_S3_SECRET_KEY` and
  `COWORK_USERCONTENT_SECRET`. A value is never in an object of this
  document or in a commit.
- **A key file** is a `files` entry whose `valueFrom.secretKeyRef`
  names a `Secret`, with `mode: 0400`, owned by the process's uid, which
  is the file's owner when none is given and `user` is that uid as
  `uid:gid`. A `Secret`'s `data` is bytes, and its keys and values
  together are at most 512 KiB (ADR 0020). This
  is the key-file rule of
  [deployment-contract.md](./deployment-contract.md) §What the binary
  enforces about key material. The files are `GARAM_CA_KEY_FILE`,
  `GARAM_MACHINE_TLS_KEY_FILE`, `GARAM_POLICY_SIGNING_KEY_FILE`,
  `COWORK_DEPLOYMENT_KEY_FILE` and `POLICY_TLS_KEY_FILE`. Gaon's one
  worked example of a file from a `Secret` is the `etcd-snapshot`
  CronJob of `deployment.md`, with `mode: 0400` and a `secretKeyRef`.
- **A public file** is a `files` entry whose `valueFrom` is a
  `configMapKeyRef` and whose mode is gaon's default of 0644: the
  machine certificate and server root, the CA files
  (`GARAM_POLICY_CA_FILE`, `COWORK_POLICY_CA_FILE`,
  `GARAM_TRANSIT_CA_FILE`), `POLICY_TLS_CERT_FILE`,
  `GARAM_IDENTITY_SERVICE_KEYS_FILE` and `POLICY_JWKS_FILE`. The mode a
  CA file needs is not established
  ([deployment-needs.md](./deployment-needs.md) §Not established).
- **Bounds.** A MicroVM takes at most 64 files and 1 MiB of file
  content in all; over either it fails as `FilesOverBound`
  (`node-agent.md`). Each process here holds fewer than ten files.
- **Probe timing.** A zero timing field takes gaon's default:
  `initialDelaySeconds` 0, `periodSeconds` 10, `timeoutSeconds` 1,
  `successThreshold` 1 and `failureThreshold` 3, and a liveness failure
  restarts the MicroVM (ADR 0020). `/readyz` of `serve api` is bounded
  at 2s and the policy service's at 2s
  ([deployment-needs.md](./deployment-needs.md) entries Probes), so the
  readiness probe of each states `timeoutSeconds` of 3, not the
  default 1. Start-to-ready time is not established, so no
  `initialDelaySeconds` is stated.
- **Probes.** The three Go and Rust images have no shell, no `wget`
  and no `curl`, so their probes are `httpGet`; `exec` would run the
  process's own `probe` command and is not used. The node-based web
  images could take either, and `httpGet` is stated for all eight so
  that one form holds. A `Service` lists only MicroVMs that are
  `Ready` (ADR 0027), so the readiness probe also decides which
  replica receives traffic. Liveness reads `/healthz`; readiness reads
  `/readyz`. A probe may name its port by name or by number
  (`resource-api.md` §MicroVM).
- **Stop.** The three `serve` commands of `garam/server` and
  `cowork serve` give in-flight work 10s on SIGTERM
  ([deployment-needs.md](./deployment-needs.md) entries Stop), and a
  grace period below 10s truncates it
  ([deployment-contract.md](./deployment-contract.md) §Termination);
  gaon's default of 30 is within both. The stop of the three web images
  and of `policy serve` is not established, so no object states a
  period for them and each takes the default.

**Per process**

| Process | Kind | Replicas | Probe port | Files | Settings routed to |
|---|---|---|---|---|---|
| `garam serve api` | `Deployment` | 1 | `GARAM_HTTP_ADDR`, 8080 by default | key: `GARAM_POLICY_SIGNING_KEY_FILE` where set | §garam/server: serve api |
| `garam serve authz` | `Deployment` | 1 | `GARAM_METRICS_ADDR`, 9091 | none | §garam/server: serve authz |
| `garam serve machine` | `Deployment` | 1 | `GARAM_MACHINE_HEALTH_ADDR`, 8444; the TLS listener is port 8443 | key: `GARAM_CA_KEY_FILE`, `GARAM_MACHINE_TLS_KEY_FILE`, `GARAM_POLICY_SIGNING_KEY_FILE` where set | §garam/server: serve machine |
| `garam migrate` | `Job` | n/a | none | none | §garam/server: migrate |
| `garam/console` | `Deployment` | 1 | 3000 | none | §garam/console |
| `garam/site` | `Deployment` | 1 | 3000 | none | §garam/site |
| `cowork serve` | `Deployment` | 1 | `COWORK_LISTEN_ADDR` port | key: `COWORK_DEPLOYMENT_KEY_FILE` where set | §garam/cowork: serve and migrate |
| `cowork migrate` | `Job` | n/a | none | none | §garam/cowork: serve and migrate |
| `garam/cowork-web` | `Deployment` | 1 | 3000 | none | §garam/cowork-web |
| `policy serve` | `Deployment` | 1 | `POLICY_HEALTH_ADDR` port | key: `POLICY_TLS_KEY_FILE` | §garam/policy: serve and migrate |
| `policy migrate` | `Job` | n/a | none | none | §garam/policy: serve and migrate |

The Files column lists key files only; the public files above are
each in the entry that reads them. Every setting is in the entry named
in the last column. Where a probe port is a setting's, the port is the
one that setting is given, and the MicroVM names it in `ports` and the
probe names it by that name.

A `Job` states `completions: 1` and a `template` that is a MicroVM
spec with the fields above (`workloads.md`). `garam migrate` leaves a
database dirty when it fails and every later run refuses, so a retry
cannot succeed and `backoffLimit: 0` is stated for it
([deployment-contract.md](./deployment-contract.md) §The migrate job).
That section's claims are `garam migrate`'s alone; the `backoffLimit`
of the two other migrations is not established. Two runs at once of
`garam migrate` are safe by the driver's advisory lock, which is why a
deployer that creates the `Job` twice does no harm.

**What a restart does to a value.** A restart of a MicroVM under
`runStrategy: Always` boots the values resolved when it was created,
so a `Secret` changed since reaches a file by gaon's in-place
replacement (`node-agent.md`, ADR 0029) but reaches an `env` entry only
at a replacement of the MicroVM, and not at a restart. That is
garamsh/gaon#170. A garam process reads its key files at start, so a
file replaced in place is read at the next start and not before; this
document does not say a rotation by restart works
([deployment-needs.md](./deployment-needs.md) §Not established).

## Order

The chart holds this order by a required value, `stage`: `migrate`
renders the three `Job`s alone and `serve` renders everything else, so
a deployer renders and applies the first, waits for the three `Job`s to
complete, and then renders and applies the second. One render is not
used because `helm template` emits a `Deployment` before a `Job`, and
`gaon apply -f -` keeps that order and has no hook
(`cli.md` §Applying a Helm-rendered stream). garamsh/gaon#201, which
asked for it, is closed with that answer in ADR 0035. The deployer's
script, which waits, is not in the chart and is not shown.

Gaon holds no order between kinds. Neither `workloads.md` nor
`deployment.md` gives a field by which a `Deployment` waits for a `Job`,
and `resource-api.md` gives none by which one object waits for another;
the answer to garam's need on garamsh/gaon#125 (the comment at
`https://github.com/garamsh/gaon/issues/125#issuecomment-6830308347`)
says the Job's status is readable and "the order of migration before
rollout is garam's". The deployer sequences it:

1. Create the three databases and the objects every process names
   ([Databases and object storage](#databases-and-object-storage)).
2. Create `garam migrate`, `cowork migrate` and `policy migrate` as
   three `Job`s. They are independent of each other.
3. Read each `Job`'s status. A `Complete` condition releases the
   `serve` objects that read that database; a `Failed` condition, which
   a `Job` takes after `backoffLimit`, stops the deployment before any
   of them is created (`workloads.md`).
4. Create `serve api`, `serve authz` and `serve machine` once
   `garam migrate` is `Complete`; `cowork serve` once `cowork migrate`
   is; `policy serve` once `policy migrate` is.
5. Create the three web images at any time after their settings exist:
   they read nothing at start
   ([deployment-needs.md](./deployment-needs.md) entries Before start).
   The local stack starts them after the api, which is that stack's
   choice and not a need.

Two gaon facts bear on step 2. A finished `Job` and its MicroVMs are
kept (`workloads.md`), so a release that runs a migration again names
its `Job` anew, since the old object holds the name. A `Job` runs one
MicroVM at a time and has no `parallelism`, which no migration here
needs.

Between the `serve` objects themselves garam states no order beyond
the migrations: [deployment-needs.md](./deployment-needs.md) lists no
process as reading another at start, only as calling it, and a
readiness probe of `serve api` reports the authz channel, so the api
is ready only when authz is reachable (§garam/server: serve api, via
[deployment-contract.md](./deployment-contract.md) §What readiness
reports). A deployer that creates the objects together lets the api
wait on its probe.

## Names

A public name is a host a person or a process opens
([deployment-needs.md](./deployment-needs.md) §Public names, which says
what must agree with each). Gaon serves no object that serves a host:
the `Gateway`, `HTTPRoute` and `TLSRoute` named below are contract
(`gateway.md`, ADR 0032), not served, and the gap table lists them.
Their group is `gateway.gaon.garam.sh/v1alpha1`.

**Public names**

| Name | Objects that serve it | Where TLS ends |
|---|---|---|
| The site | a `Service` for `garam/site` on port 3000 and an `HTTPRoute` on the site's host, path prefix `/` | at the gateway |
| The console | a `Service` for `garam/console` on port 3000 and an `HTTPRoute` on the console's host, path prefix `/` | at the gateway |
| cowork's host | `Service`s for `cowork serve` and `garam/cowork-web` and one `HTTPRoute` on one host with three rules: path prefix `/api/` to cowork with a `URLRewrite` of `ReplacePrefixMatch` to `/`, `/mcp` to cowork unchanged, and `/` to cowork-web | at the gateway |
| The user-content origin | a `Service` for `cowork serve`'s `COWORK_USERCONTENT_ADDR` port and an `HTTPRoute` on its own host | at the gateway |
| The machine listener | a `Service` of `type: LoadBalancer` for `serve machine`'s port 8443, or a `Gateway` listener of protocol `TLS` with `Passthrough` and a `TLSRoute` that selects it by server name | in `serve machine`, which presents the certificate of `GARAM_MACHINE_TLS_CERT_FILE` |
| The identity issuer | `/api` on the site's host: the route above reaches `garam/site`, which proxies `/api/*` to the api | at the gateway |
| The token endpoint | `serve machine`'s `/oauth/token`, on the machine listener's host | in `serve machine` |

- **The gateway is contract, not served.** `gateway.md` at
  `cfb148dea568674525207bd329a94f1d9ccc912f`, read in full, states
  three kinds and ADR 0032 decides them; gaon has no unit that serves
  them.
  - A `Gateway` has `listeners` (a `name`, a `port`, a `protocol`, a
    `hostname` and `tls`) and at most one of `addresses`. A port holds
    one protocol. `HTTPS` takes `tls.mode: Terminate` and one
    `certificateRefs` entry naming a `Secret`; `TLS` takes
    `Passthrough` and no certificate.
  - An `HTTPRoute` has `parentRefs` (a `name` and a `sectionName`),
    `hostnames` and `rules`. A rule has `matches` of a `PathPrefix`,
    at most one filter, `URLRewrite` with `ReplacePrefixMatch`, which
    takes exactly one match, and exactly one backend, a `Service`
    `name` and `port`.
  - A `TLSRoute` has the same references and exactly one rule of one
    backend.
  - `Host` and the path are forwarded unmodified unless a rewrite is
    declared. A certificate is a `Secret` with the keys `tls.crt` and
    `tls.key`. There is no `GatewayClass`, no `allowedRoutes` and no
    namespace.
  The chart renders these kinds behind `gateway.enabled`, which is off
  by default; with it on, the output is what the contract states, not
  what a cluster has shown to work.
- **The machine listener** ends TLS in the process, with the
  certificate of `GARAM_MACHINE_TLS_CERT_FILE`, so a gateway that
  terminated it would present another. A `LoadBalancer` `Service` is L4
  and passes the connection through (`resource-api.md` §Service,
  Endpoints and NetworkPolicy); a `TLS` listener with `Passthrough`
  and a `TLSRoute` is the other form. The chart renders the `Service`
  and, behind `gateway.enabled`, the listener and the route.
- **The route to the three web images** need not split `/api`: the
  console and the site proxy `/api/*` themselves
  ([deployment-needs.md](./deployment-needs.md) §Public names).
- **The names a person types** are the host names of the gateway
  routes; which addresses they resolve to belong to the cluster owner.
  `dns.md` covers what a MicroVM resolves, not these.
- **A gateway can hold a LAN address** through a `Gateway`'s
  `addresses`, or a `LoadBalancer` `Service` that names
  `loadBalancerIP` (`resource-api.md`). The `Service`'s address is
  served (garamsh/gaon#182, closed); announcing it on the LAN is the
  network plugin's, and is not shown.
- **What codehub reaches** is the policy listener, `/oauth/token` and
  `/oauth/introspect` on the api, and the issuer's discovery document
  and key set; it is a caller, and its needs are in
  [deployment-needs.md](./deployment-needs.md) §codehub, not restated
  here.

**Internal addresses**

A `Service`'s name is a DNS label of at most 63 characters starting
with a letter (`resource-api.md`). The names below are chosen by this
document and are the deployment's to change; each is the `Service`
name, and the address the settings carry is that name in full,
`<service>.default.svc.<zone>`, with `cluster.local` as the default
zone (`dns.md` §Names). The short form is not resolved, as the Guest
Agent writes no search line, so no setting carries it.

| Name | Serves | Carried by, as `<name>.default.svc.<zone>` |
|---|---|---|
| `garam-api` | `serve api` | `GARAM_API_URL` on the three web images; `COWORK_PLATFORM_URL` on cowork |
| `garam-authz` | `serve authz`'s gRPC port | `GARAM_AUTHZ_ADDR` on the api and on `serve machine` |
| `garam-policy` | `policy serve`'s contract listener | `GARAM_POLICY_ADDR` and `COWORK_POLICY_ADDR` |
| `garam-cowork` | `cowork serve`'s API port, 8090 | `COWORK_API_URL` on cowork-web, as `http://<name>.default.svc.<zone>:8090` |

- A `Service` is selected by labels, so each `Deployment`'s template
  carries the label the matching `Service`'s selector names. Port names
  in a MicroVM and a `Service`'s `targetPort` agree.
- **A name that is also in a certificate.** The policy service
  presents a certificate that `GARAM_POLICY_CA_FILE` and
  `COWORK_POLICY_CA_FILE` trust, so the name in `GARAM_POLICY_ADDR` and
  `COWORK_POLICY_ADDR` must be one that certificate names. That name
  is `garam-policy.default.svc.<zone>`, so the policy service's
  certificate must name it. The same holds for any certificate a
  garam process presents to another by a `Service` name: it names that
  `Service`'s full name, as the caller connects to the full name. That
  [deployment-needs.md](./deployment-needs.md) §garam/policy does not
  establish which names the certificate carries is why this is stated
  here.
- **A setting names a `Service`, not a MicroVM.** A `Service` is served
  at the commit above, so a setting no longer takes the MicroVM's
  `status.addresses`. That a MicroVM's resolver reaches the DNS unit is
  not shown (§How a claim about gaon is read): the name is the right
  one to carry, and a cluster where it does not resolve yet is the gap
  table's DNS entry.

## Databases and object storage

What the deployment must give, in the terms of
[deployment-needs.md](./deployment-needs.md) §Databases; not how aurora
runs them.

- **Three PostgreSQL 17 databases**, one for garam, one for cowork and
  one for the policy service, each with a role that owns its objects;
  the URL of each is a `Secret` key read as an `env` entry.
- **A session connection** for `LISTEN` on garam's database, which a
  pooler that does not keep one breaks.
- **An address for each, reachable from the MicroVMs** that read it,
  carried in the URL. Which name it takes belongs to whatever serves the
  database, and is not a `Service` of this document.
- **An S3-compatible bucket for cowork** where cowork's S3 settings are
  set, with an access key and secret key as `Secret` keys. Which of
  cowork's S3 settings are required, and the operations it calls, are
  in [deployment-needs.md](./deployment-needs.md) §garam/cowork: serve
  and migrate.

No gaon object declares a database or a bucket. A `StatefulSet` and
a `Volume` would hold database files, but a `Volume` is never deleted
by the workload kinds (`workloads.md`), a volume's `mountPath` mounting
is not done (`node-agent.md`), and the storage contract of
garamsh/gaon#117 (D7) is not filed. None of that is needed to give
garam a database that someone else runs.

## The Transit arrangement

None. `serve machine` is given no `GARAM_TRANSIT_ADDR`, and with it
none of the other `GARAM_TRANSIT_*`, which the process refuses to
have beside no address
([deployment-needs.md](./deployment-needs.md) §garam/server: serve
machine). Message signing then seals its private keys under the
key-encryption key, which is the bytes of `GARAM_CA_KEY_FILE`: a key
file from a `Secret` as above, and one that cannot be regenerated
([deployment-contract.md](./deployment-contract.md) §The key-encryption
key cannot be regenerated).

A deployment that gives Transit uses one of two forms of
[deployment-contract.md](./deployment-contract.md):

- **The token file form,** `GARAM_TRANSIT_TOKEN_FILE`, a key file from a
  `Secret` that something beside garam writes and renews. Gaon has no
  object whose job is that, so the token is the deployer's.
- **The identity file form** of
  [ADR-0152](./adr/0152-garam-may-log-in-to-openbao-with-a-workload-identity-file-beside-the-token-file.md),
  `GARAM_TRANSIT_IDENTITY_FILE`, which needs a `ServiceAccount` named by
  `serviceAccountName` on the MicroVM, so that the Node Agent writes the
  token at `/var/run/secrets/gaon.garam.sh/serviceaccount/token`, owner
  as a file that names none, mode 0600 (`node-agent.md`, ADR 0029).
  `GARAM_TRANSIT_IDENTITY_FILE` names that path; no `files` entry may,
  as an entry at that path fails the MicroVM as `InvalidSpec`.

What the second form needs of gaon is garamsh/gaon#161, which serves
the issuer's OpenID discovery document and key set, and
garamsh/gaon#162, which writes the token as a file and renews it in
place. Both are closed: #161 was done in #175 as `1312f67` and #162 in
#179 as `0ef1363`, and their threads were read. At
`9941e390da42b49071ded0508f953ccac60e6e58`, the commit that
[deployment-contract.md](./deployment-contract.md) §A Transit provider
for message signing keys cites, they were open.

What ADR 0029 and `node-agent.md` document at the commit above for the
token file:

- the path `/var/run/secrets/gaon.garam.sh/serviceaccount/token`;
- mode 0600;
- the owner of a file that names none: the MicroVM's `user` when it is
  a numeric `uid:gid`, root when there is no `user`, and `InvalidSpec`
  when it is a name;
- renewal by `WriteFile` before 80% of the token's lifetime, which
  mvmd's guest agent at `223e3eca764caf60b8dd72c7399b83ff6b437d49`
  does by writing a temporary file, syncing and renaming it, so a
  reader never sees a partial token;
- after a start that is not the first, the guest holds the token of
  the create-time until the next resync, as `env` and `files` are not
  yet resolved again at a start (garamsh/gaon#170, waiting on
  garamsh/mvmd#519).

The Node Agent also needs `--service-account-audience` set to one of
the API server's audiences, and without it a MicroVM that names a
ServiceAccount is not created (`node-agent.md`); that is a cluster
setting, not an object of this document. Which role and which issuer
OpenBao's JWT method trusts are the deployment's, as in
[deployment-contract.md](./deployment-contract.md).

## What gaon does not yet serve

An entry names a need above, the gaon issue that tracks it where one
exists, and what a deployment does until it is served. Issue states are
those read when this was written. "No gaon issue" means none was found
among the open and closed issues of `garamsh/gaon` listed with their
titles, and is for the PM to file; no number is invented here.

| Need | Gaon issue | Until it is served |
|---|---|---|
| The kinds `Gateway`, `HTTPRoute` and `TLSRoute` served: hosts, the `/api/` prefix rewrite, TLS from a `Secret`, passthrough by server name | contract in `gateway.md` (garamsh/gaon#200, ADR 0032); no `gateway/` unit at the commit above; garamsh/gaon#222 and #223 were opened for one on 2026-11-13 (01:24Z), and when read #223 was open and #222 closed (02:53Z), neither thread read | the chart renders the kinds behind `gateway.enabled`, off by default; with it off the deployer runs a TLS-terminating proxy of its own with the host and path split of §Names |
| A MicroVM resolving a `Service`'s name | `dns.md` (garamsh/gaon#203, ADR 0033); the `dns/` unit exists; garamsh/gaon#196, Service addresses in the network plugin, open at the commit above | every address is the full `<service>.default.svc.<zone>` name; that a MicroVM resolves it is not shown, and a deployment where it does not carries the address of the MicroVM |
| A `LoadBalancer` address announced on the LAN for the machine listener | garamsh/gaon#196, open at the commit above | the `Service` names `loadBalancerIP`; that the address answers on the LAN is not shown |
| `env` and `files` resolved again at each restart, and `workingDir` | garamsh/gaon#170, open at the commit above (waits on garamsh/mvmd#519) and closed 2026-11-13 02:44Z, by a change not read | a changed `Secret` reaches a key file by in-place replacement and an `env` entry only at a replacement of the MicroVM; no object here sets `workingDir` |
| `nodeSelector` binds only to Nodes whose labels match, by `e72e153f30f67c2e320ccab16eaeeb79b5ab645c` | garamsh/gaon#181, closed | none of the objects here states a `nodeSelector` |
| Spread of a replicated workload across Nodes | no gaon issue found (decided in a comment on #117, not in `scheduler.md`) | every `Deployment` is one replica, so none is needed |
| A `Deployment`'s minimum ready time, or a ReplicaSet | no gaon issue found | `Recreate`, as above |
| Order between kinds: a `Deployment` that waits for a `Job`, and hooks in an applied stream | garamsh/gaon#125 gives the order to garam; garamsh/gaon#201, closed, answers that `apply` keeps Helm's order with no hooks (ADR 0035) | the chart's `stage` value gives two renders that the deployer applies in turn, as in §Order |
| A `Job` with `parallelism` | no gaon issue found | none needed |
| `OnDemand` run strategy, and a volume's `mountPath` mounting | no gaon issue read | no object here uses either |
| A `Deployment`'s `maxSurge` and `maxUnavailable` as an integer | garamsh/gaon#186, closed (`34d75c3170c239a1b2b96f2efd41ef3eb6b7318b`) | no object here states them, `Recreate` being used |

None of these blocks writing the objects of this document: each
object above is stated so that it does without. `resources` is not in
the table because it is not a need gaon lacks but one it states and the
objects must meet (§Not established).

The identity file form's two issues, garamsh/gaon#161 and #162, are not
in the table: they are closed.

garamsh/gaon#117, #125, #159, #161, #162, #164 and #170 were read with
their comments when this document was first written, and #201 when it
was revised. The state of every other issue named is that read from the
issue on 2026-11-13, and its thread was not read; garamsh/gaon#196 was
open at the commit above and was closed afterwards, by a change this
document does not read.

## Not established

Not guessed. Each is what the documents read do not state.

- **Replica safety above one** for every process, and failover. Both
  are [deployment-needs.md](./deployment-needs.md) §Not established
  and are not answered by gaon.
- **The stop period** of the three web images and of `policy serve`.
- **The figure of a process's resources.** That `resources` is
  required is established: at the commit above gaon's node agent fails
  a MicroVM that names no cpu or no memory (`InvalidSpec`), sizing it
  by the limits and, with none, the requests, and the scheduler places
  by `Requests`. Every object of the chart therefore carries
  `resources.requests.{cpu,memory}` and optionally `limits`. The chart
  has no default and the repository states no figure:
  [deployment-needs.md](./deployment-needs.md) §Not established
  states none, so the deployment supplies one per object.
- **A read-only root filesystem.** Not stated in the MicroVM fields
  read.
- **How the machine listener and the issuer are reached from outside
  the cluster.** That is the cluster owner's and the gateway's, and
  the gateway is contract only ([Names](#names)).
- **That the chart applies to a gaon cluster.** Nothing applies it;
  what is shown is its render, checked against the fields gaon's
  files name.
- **The gid of `user`.** `user` is `uid:gid`; the group of the
  account is in the image's group file and is not read here.
- **Whether a MicroVM's `status.addresses` carries one address or
  more**, and which a deployer reads when it carries several.
