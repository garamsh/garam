# garam-gaon Helm chart

This chart governs the gaon objects that describe a garam deployment on
a gaon cluster; `deploy/charts/garam/` describes the same deployment
for Kubernetes and is not touched by it.

## What this is

A template for the objects
[deployment-gaon.md](../../../docs/architecture/deployment-gaon.md)
states, so a deployment supplies values and writes no template of
garam's. Helm is used as a template engine and nothing else:

```
helm template RELEASE deploy/charts/garam-gaon --values ... | gaon apply -f -
```

**Nothing here has been applied to a gaon cluster.** The chart is
checked against what gaon's documents say a cluster accepts (§How the
rendered objects are checked), and no run of `gaon apply` on its output
has been made. Whether a cluster accepts it is not shown.

The chart creates no `Secret` and no `ConfigMap` (ADR-0042). Every
`secret` and `configMap` value is the name of an object the deployment
creates, with the key inside it, and no value of either passes through
the chart.

## What it renders

Eight `Deployment`s of `replicas: 1` and `strategy.type: Recreate`
(ADR-0042 holds a replica count of one until replica safety is
established), a `Service` for each, and three `Job`s. A `Deployment` or
`Job` is of `workloads.gaon.garam.sh/v1alpha1` and a `Service` of
`compute.gaon.garam.sh/v1alpha1`.

| Object | Runs | Port names |
|---|---|---|
| `<prefix>-api` | `garam serve api` | `http` 8080 |
| `<prefix>-authz` | `garam serve authz` | `grpc` 9090, `health` 9091 |
| `<prefix>-machine` | `garam serve machine` | `mtls` 8443, `health` 8444 |
| `<prefix>-policy` | `policy serve` | `grpc` 8443, `health` 8091 |
| `<prefix>-cowork` | `cowork serve` | `http` 8090, `usercontent` 8092 |
| `<prefix>-console` | `node /app/platform-web/server.js` | `http` 3000 |
| `<prefix>-site` | `node /app/site-web/server.js` | `http` 3000 |
| `<prefix>-cowork-web` | `node /app/cowork-web/server.js` | `http` 3000 |
| `<prefix>-migrate-<run>` | `garam migrate` | none |
| `<prefix>-cowork-migrate-<run>` | `cowork migrate` | none |
| `<prefix>-policy-migrate-<run>` | `policy migrate` | none |

A `Service` exposes the port named in its row, except that the health
ports of `authz`, `machine` and `policy` are for the probes alone and
no `Service` carries them. Where `gateway.enabled` is true the chart
also renders one `Gateway`, four `HTTPRoute`s and one `TLSRoute`
(§What gaon does not yet serve).

Every object carries `resources`: gaon's node agent fails a MicroVM
that names no cpu or no memory, so the field is required, per object
(`resources.<object>.requests.{cpu,memory}`), with `limits` optional.
The chart has no default and no figure is written in this repository
outside `ci/`, whose figures are fixtures (`deployment-gaon.md` §Not
established). The chart sets no `nodeSelector` or `workingDir`, and no
Transit setting: gaon's documents show a place for the first two that
nothing in garam's documents fills, and the third is not served
(below).

## Order

Migrations come before services. Helm gives no order gaon honours:
`gaon apply` takes the stream as Helm emits it, does not reorder or
retry it, and has no hooks, and Helm emits a `Deployment` before a
`Job` whatever the templates' file names. A single render would start
the services before the migrations had run.

So the chart renders one half at a time, by the required value `stage`:

1. `stage: migrate` renders the three `Job`s and nothing else. Apply it
   and wait until all three have completed.
2. `stage: serve` renders the `Service`s, the `Deployment`s and, where
   on, the routes, in the order Helm emits them: a `Service` before the
   `Deployment` that it selects, the routes after both.

A finished `Job` and its MicroVM are kept, so each run of the first
stage needs `Job`s of new names: `migrate.run` is required and ends each
name. A release that migrates again sets a new `migrate.run`; the
previous run's `Job`s stay until the deployer deletes them. The same
`migrate.run` with a new image digest therefore runs nothing: the
finished `Job` of that name is kept, and no new one starts. Change
`migrate.run` with the digest.

Where `gateway.enabled` is true the serve stream holds kinds that
`gaon apply` does not know at the commit below, and it refuses the
whole stream and sends nothing (`cli.md` §Apply and §Applying a
Helm-rendered stream). Leave it off until a gaon serves the gateway.

`garam migrate` leaves a database dirty when a migration fails, and
every later run refuses it, so its `Job` sets `backoffLimit: 0`: a
second attempt would fail for that reason alone. gaon#201 is closed,
and gaon's ADR 0035 and `cli.md` state that `apply -f -` keeps Helm's
install order and offers no hooks, prune or history, at
cfb148dea568674525207bd329a94f1d9ccc912f. So the two renders are the
mechanism, and a deployer that scripts the two applies is the one that
holds the order. `ci/` renders both stages, and `make -C deploy/charts
gaon-order` holds each render to the kinds of its stage and to Helm's
install order.

## Names

An object is named `<namePrefix>-<word>`; gaon has no namespaces, so
the prefix is what keeps two deployments' objects apart. A name is held
to 60 characters, because a `Service` name is a DNS label of at most 63
and a replica's MicroVM adds `-<index>`.

A `Service` is reached at `<service>.default.svc.<dnsZone>`. `dnsZone`
defaults to `cluster.local`. The short form is not resolved and no
search line is written (gaon's `dns.md`), so every address the chart
renders is the full name, and a caller outside the chart uses it too.

A certificate a garam process presents to another by a `Service` name
must name that full name. The policy service's must name
`<namePrefix>-policy.default.svc.<dnsZone>`, which is what `api` and
`cowork` dial, or its callers refuse the connection.

## Values a deployment must supply

`values.yaml` carries every value and a comment on each. A value that
has no default is `required`, and a render without it fails with a
message that opens with the value's dotted name.

| Group | Values |
|---|---|
| Which half | `stage`, `migrate.run` |
| Names | `namePrefix`, `dnsZone` |
| Resources | `resources.<api,authz,machine,policy,cowork,console,site,coworkWeb,migrate,coworkMigrate,policyMigrate>.requests.{cpu,memory}`, a quantity such as `500m` or `1Gi`; optionally `.limits.{cpu,memory}` |
| Images | `image.<server,cowork,policy,console,site,coworkWeb>.{repository,digest,user}`; `repository` holds no tag, the digest is `sha256:` and 64 hexadecimal digits, and `user` is `uid:gid` |
| Databases | `database.<garam,cowork,policy>.{secret,key}` |
| Public origins | `public.{siteUrl,consoleUrl,coworkUrl}`, and `public.coworkSyncUrl`, which is not an origin: a ws or wss URL of cowork's server under `/api` on cowork-web's own host; optionally `public.coworkWebUrl` |
| Identity | optionally `identity.issuer` (`https://<host>/api`, equal to `policy.tokenIssuer`, never `machine.issuer`) and `identity.serviceKeys.{configMap,key}`, which need `machine.tokenUrl` ending `/oauth/token`; `cowork.policy` needs both |
| policy | `policy.tokenIssuer`, `policy.jwks`, `policy.tls.{cert,key}` |
| api | `api.policy.{ca,signingKey}`; optionally `api.sessionTtl`, `api.cookieSecure`, and `api.google`, all of it or none |
| machine | `machine.{issuer,tlsCert,tlsKey,serverRoot,caKey}`; optionally `machine.tokenUrl`, `machine.agentTokenLifetime`, and `machine.service.{type,loadBalancerIP}` |
| cowork | `cowork.webOrigins` (host patterns such as `*.garam.example`, not URLs), `cowork.s3.{bucket,accessKey,secretKey}`, `cowork.userContent.{origin,secret}`; optionally the rest of `cowork.s3` and `cowork.policy`, all of it or none |
| Routes | `gateway.enabled`, off; where on, a host and a certificate `Secret` for each of the site, console, cowork and user-content origins, and a host for the machine listener |

The key files are `files` entries of mode `0400` from a `Secret`; the
public ones come from a `ConfigMap` in gaon's default mode. The
migrations of garam and cowork ship in their images at `/app/migrations`
and are named in full (`GARAM_MIGRATIONS_DIR`, `COWORK_MIGRATIONS_DIR`),
because the image's working directory is not passed (below); the policy
service's are compiled into its binary.

## Not set by this chart

Every environment variable the source reads is either rendered or
listed here with the reason. `make -C deploy/charts ci` compares the
two sets against the source and fails on a name it finds in neither or a
name here that the source no longer reads. A name rendered is held to
the source of its own process for cowork, the policy service and each
of the three web apps; the three `garam serve` objects and the three
migration `Job`s share the platform's one set, so a name that belongs to
one of them and is rendered on another is not caught.

| Variable | Why not |
|---|---|
| `GARAM_ADAPTER_AGENT` | The adapter is not one of garam's workloads: it runs beside one agent, deployed by whoever runs the agent (`docs/architecture/adapter.md`). |
| `GARAM_ADAPTER_MACHINE_URL` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_GATEWAY_URL` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_TOKEN_FILE` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_CONTROL_TOKEN_FILE` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_SERVER_ROOT_FILE` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_CONTROL_URL` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_CONTROL_ROOT_FILE` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_PLACEMENT_TOKEN_FILE` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_OUTBOX_DIR` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_CONTROL_SOCKET` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_SIGN_MESSAGES` | Same: the adapter's own deployment. |
| `GARAM_TRANSIT_ADDR` | None of the Transit arrangements is rendered: `deployment-gaon.md` §The Transit arrangement states none, and `serve machine` refuses any `GARAM_TRANSIT_*` beside no address. |
| `GARAM_TRANSIT_AUTH_MOUNT` | Same: it belongs to the identity file form, which needs a `ServiceAccount` no object of this chart names. |
| `GARAM_TRANSIT_AUTH_ROLE` | Same: it belongs to the identity file form. |
| `GARAM_TRANSIT_CA_FILE` | Same: Transit is not rendered. |
| `GARAM_TRANSIT_IDENTITY_FILE` | Same: the identity file form is not rendered. |
| `GARAM_TRANSIT_MOUNT` | Same: Transit is not rendered. |
| `GARAM_TRANSIT_SIGNING_KEY` | Same: Transit is not rendered. |
| `GARAM_TRANSIT_TOKEN_FILE` | Same: gaon has no object whose job is to write and renew the token, so the token form is the deployer's. |

## What gaon does not yet serve

Read from gaon at `cfb148dea568674525207bd329a94f1d9ccc912f`
(`deployment-gaon.md` §What gaon does not yet serve states each with
its issue).

- **The gateway.** `gateway.md` states the kinds `Gateway`,
  `HTTPRoute` and `TLSRoute` of `gateway.gaon.garam.sh/v1alpha1`. It is
  a contract: that commit holds no `gateway/` unit, and garamsh/gaon#223
  was open on 2026-11-13 to serve it. The routes render
  only where `gateway.enabled` is true, and what they say is the
  contract's, not something a cluster serves. Without them the origins
  of `deployment-needs.md` §Public names are reached as the cluster
  owner decides.
- **Names resolved in a MicroVM.** `dns.md` states
  `<service>.default.svc.<zone>`, and a `dns/` unit (`gaon-dns`) exists
  at that commit; `dns.md` says a MicroVM resolves nothing until the
  network plugin implements Service addresses (garamsh/gaon#196, open
  at the commit above and closed 2026-11-13 02:06Z, by a change not
  read).
  That the unit's resolver is reached from a MicroVM is not shown.
- **`workingDir` and `env` and `files` resolved again at a restart**
  (garamsh/gaon#170, open at the commit above and closed 2026-11-13
  02:44Z, by a change not read). No object sets `workingDir`, so the web
  images start by `node` with an absolute path and the migrations are
  named in full; a changed `Secret` reaches a key file by in-place
  replacement and an `env` entry only at a replacement of the MicroVM.
- **Transit.** Not rendered (above).

## How the rendered objects are checked

The renders are not parsed by gaon's types, because that needs a Go
module here: a `go.work` and a dependency-bot entry outside the chart's
paths. It would reach `MicroVMSpec` and `Service`, which are in gaon's
importable `lib/api`, and not the wrapper fields of a `Deployment` and
a `Job` (`replicas`, `selector`, `strategy`, `completions`,
`backoffLimit`, `template`), which are in `workloads/internal/job`,
nor the gateway kinds, which no unit serves. Instead
`ci/fields.allow` lists every field path the renders may set, each
compared by hand with the gaon file that states it, and `make -C
deploy/charts gaon-fields` fails on a rendered path it does not hold
and on an entry nothing renders. What it does not reach is below.

## What the checks are complete over

`make -C deploy/charts ci` runs, beside the garam chart's checks:

| Target | Reaches | Does not reach |
|---|---|---|
| `gaon-lint` | `helm lint --strict` of the chart under each value file | a value file that is not in `ci/` |
| `gaon-render` | environment names the source reads against the names rendered or declared unset, each name held to its own process's source for cowork, policy and each web app; every address a process dials is a full Service name | a name on the wrong one of `serve api`, `serve authz`, `serve machine` and the three migration Jobs, which share one set; a name read through a path the sweeps do not enumerate; the correctness of a value |
| `gaon-order` | each render holds the kinds of its stage, in Helm's install order; counts of Deployments, Services and Jobs | that gaon applies in that order |
| `gaon-fields` | every field path rendered, against `ci/fields.allow` | a value's type or range, and any field the allowlist's source files do not state |
| `gaon-guards` | each `ci/refused/` case is refused with the value's name; the value files still render | a missing value no case names |
| `gaon-names` | object names at most 63 characters under a long prefix; an over-long prefix refused | a name an apply refuses for another reason |
| `gaon-secrets` | no `Secret` or `ConfigMap` rendered; each credential variable is a reference; no PEM block or password URL in the chart | a credential in no such shape |

The sweeps of the platform and the policy service's environment, and
the guards that they read through one place, are the garam chart's
`chart-render`; `gaon-render` reuses its sets and adds cowork's.

Not checked anywhere: that a cluster accepts the objects, that
`helm template | gaon apply -f -` runs, and that a MicroVM started from
them works.
