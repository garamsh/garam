# ADR 0056: A published web image is bound to the release name `garam`, and `make ci` checks the binding

> Status: accepted
> Date: 2026-09-02

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

The browser reaches the backend under `/api/*` on the web app's own
origin, so the session cookie stays same-origin and no CORS layer is
needed ([ADR-0009](./0009-web-frontend.md)). The address that rewrite
points at is fixed when the image is built.
[ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
§Consequences records it arriving as `ARG GARAM_API_URL` in
`deploy/local/Dockerfile.web`, says a release "either fixes one value for
every consumer of that image or the value becomes a runtime one first",
and leaves the choice to the change that builds the release. #792 made
it on 2026-09-01 (593f5c3) — `.github/workflows/release.yml` bakes
`GARAM_API_URL=http://garam-api:8080` — and no ADR recorded it, so the
choice ADR-0039 deferred has been written in a workflow comment and
nowhere a decision is looked for.

What that value names is one release. `deploy/charts/garam` names every
object after the release, and the api Service is `garam-api` under a
release named `garam` and `<release>-garam-api` under any other. Read on
2026-09-02, `helm template <release> deploy/charts/garam --values
ci/minimal-values.yaml`:

| Release name | api Service |
|---|---|
| `garam` | `garam-api` |
| `prod-garam` | `prod-garam-garam-api` |
| `garam-production-eu-west-1-primary-tenant-alpha-01234` | `garam-production-eu-west-1-primary-tenant-alpha-01234-garam-api` |

The third is `chart-names`' release name, which that check renders under
to prove every object name stays inside 63 characters. So one check
proves the chart survives a 53-character release name while the image it
deploys serves exactly one, and neither reads the other. Both statements
are true, which is why re-measuring either finds nothing.

The failure is silent. Under any release name but `garam` the rewrite
reaches a name that resolves to nothing: every object is `Ready`, the
web app's probes read nothing outside the process
([ADR-0023](./0023-web-probes.md)), and the first report is a console
that shows nothing.

Two more facts, both read on 2026-09-02:

- **The address does not move at runtime.** `make image-build` produced
  `garam-web:ci`, whose `.next/routes-manifest.json` carries
  `http://garam-api:8080/:path*` for `/api/:path*`. Run with
  `GARAM_API_URL=http://other-host:9999` and a live server at that
  address, `GET /api/healthz` reached the baked host, not the one the
  environment named. Next.js 16.3.1, the version `web/package.json`
  pins.
- **Nothing else bakes an address.** `deploy/local/Dockerfile` declares
  no `ARG`, and every address the server processes read is an
  environment name declared in `internal/cli/cli.go` — `serve api`
  reaches the engine at `GARAM_AUTHZ_ADDR`, which `templates/api.yaml`
  renders from the same helper the Service name comes from, so it
  follows the release name. `garam-authz` and `garam-machine` occur in
  `internal/cli/observability.go` as the service name telemetry carries,
  which nothing dials. The web image is the only one bound to a name.

The deployment that motivated this is not evidence the binding holds.
#861 records `garamsh/gitops` `manifests/garam/api.yaml:95` naming its
api Service `garam-api` in raw manifests carrying no release prefix, and
the published digest built with `http://garam-api:8080`. They agree
because two strings happen to match; nothing there records that the
image depends on that name, and `garamsh/gitops` #216 asks whether to
adopt this chart — at which point the release name stops being a
constant and becomes a value someone chooses.

## Decision

- **The `/api/*` rewrite target stays baked into the web image.** It is
  what ADR-0009 asked the framework for, and the measurement above is
  what the alternative costs: the value does not move at runtime, so
  making it move means this project writing and maintaining the proxy
  the framework already carries.
- **The value a release bakes is the api Service a release named `garam`
  renders**, in the short form a Service resolves to inside a namespace:
  `http://garam-api:8080`. It is not a free string. It is derived from
  what the chart renders, and it names the api Service and its port
  rather than a host chosen to look like one.
- **`make ci` holds the two ends together.** `chart-rewrite` compares
  the `GARAM_API_URL` `.github/workflows/release.yml` bakes against the
  api Service `deploy/charts/garam` renders under the release name
  `garam`, and renders again under `chart-names`' release name as the
  control: the two renders must differ, so equality under `garam` is
  read rather than assumed. A rename on either side fails the check
  instead of reaching a registry.
- **The chart states the constraint where a release name is chosen.**
  `deploy/charts/garam/README.md` §Choosing a release name carries it,
  rather than the §Pinning a version paragraph a deployer reaches only
  when choosing an image tag, and §What the checks are complete over
  says which half of the binding no check here reaches.
- **A deployment running a published image installs under the release
  name `garam`, or makes `garam-api` resolve to its own api Service.** A
  deployment that builds its own web image bakes its own address and is
  bound to that one instead; nothing here constrains it.

## What this does not decide

- **Whether a later release bakes something else.** This fixes what the
  value is derived from, not the value for all time. A release that
  changes it changes what release name a published image serves, which
  is this decision again and a new ADR.
- **Whether `garamsh/gitops` adopts the chart.** Its #216 asks that.
  What this supplies is the cost measured on their side of the question,
  not an answer to it.
- **How a deployment makes `garam-api` resolve where it will not name
  its release `garam`.** The chart renders no alias and this ADR
  requires none.

## Consequences

- **The binding is checked in this repository and nowhere else.**
  `chart-rewrite` reads a workflow and a render, both here. The api
  Service name in `garamsh/gitops`' raw manifests is outside it, and
  renaming it there still reports nothing — what changes is that this
  repository can no longer drift from the value it publishes without
  failing a check.
- **`chart-names` keeps its guarantee and loses an implication.** Every
  object name stays inside 63 characters under a 53-character release
  name; the image deployed alongside does not follow the name, and the
  README now says so rather than leaving the reader to infer freedom
  from a passing check.
- **A deployer who ignores the README fails in a browser.** No render,
  no probe and no log line reports it. That is why the statement moved
  to the section a release name is chosen in, and it is the residual
  cost of keeping the value baked.
- **`v0.1.0` is unaffected and stays correct for a release named
  `garam`.** The tags are immutable, so a change to the baked value is
  the next version and never a re-push (ADR-0039).
- **A second build argument that names a cluster object would need the
  same treatment.** The web image's is the only one today, and
  `chart-rewrite` covers that one by name rather than by pattern.

## Rejected alternatives

- **Bake the fully-qualified name,
  `http://garam-api.garam.svc.cluster.local:8080`.** It is what
  `deploy/local/minikube` builds with, and it binds strictly more: the
  release name, plus the namespace, plus the cluster domain. A short
  name serves a release named `garam` in any namespace.
- **Make the rewrite target a runtime value.** Measured above: it does
  not move, so this means replacing the framework's rewrite with code
  here — a catch-all Route Handler under `/api`, or a rewrite in
  `web/src/proxy.ts`. `docs/convention/stack-nextjs.md` rules against
  both shapes: §6 makes Route Handlers external-use only, and §11 names
  a general middleware hub an anti-pattern and excludes `/api` from the
  matcher. Both put every browser call to the backend — the OAuth round
  trip and the `Set-Cookie` a sign-in carries included — through a proxy
  this project would own, test and keep streaming correctly, to remove a
  constraint one line in a README states.
- **Refuse the render under any release name but `garam`.** It makes the
  chart enforce the image's constraint, and it refuses deployments the
  constraint does not reach: one that builds its own web image is bound
  to its own address, and the chart cannot tell which image a value
  names.
- **Render an alias Service under the fixed name `garam-api`.** It would
  make a published image work under every release name, and it puts an
  object in every release whose name cannot be scoped — two releases in
  one namespace collide on it, and a deployment that built its own image
  carries it for nothing.
- **Leave the constraint in `release.yml`'s comment and ADR-0039
  §Consequences.** Both are accurate and neither is read when a release
  name is chosen. #861 measured what that costs: the only deployment
  that works with the released image works by coincidence.
- **Drop the rewrite and have the browser call the api's own origin.**
  ADR-0009 chose the rewrite so the `garam_session` cookie stays
  same-origin and no CORS layer is needed; this would trade a build-time
  address for that layer.
