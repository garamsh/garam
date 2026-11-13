# ADR 0150: A web image reads the API address when it starts, and a published image is bound to no release name

> Status: accepted; superseded in part by
> [ADR-0156](./0156-a-release-publishes-eight-images-and-a-pull-request-builds-the-guest-only-to-its-build-stage.md)
> (a release publishes six images)
> Date: 2026-11-13

Drafted for #1810, which reserves this number. It supersedes
[ADR-0056](./0056-a-published-web-image-is-bound-to-the-release-name-garam-and-make-ci-checks-the-binding.md)
in full, and in part
[ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
§Consequences ("A published web image is bound to one API URL") and
[ADR-0134](./0134-an-image-lives-at-its-publishing-repository-and-its-tag-says-what-a-build-is.md)
§Decision 5 ("The site bakes the address the console bakes"). ADR-0071
holds no passage on the address: what it says of a development build is
its tag and its repository, and those stand.

## Context

The browser reaches the backend under `/api/*` on the web app's own
origin, so the session cookie stays same-origin and no CORS layer is
needed ([ADR-0009](./0009-web-frontend.md)). ADR-0056 recorded that the
rewrite's target was fixed when the image was built: `next build`
evaluates `rewrites()` in `next.config.ts` and writes the result into
`routes-manifest.json` and the standalone `server.js`, so
`GARAM_API_URL` at run time moved nothing. A published console or site
image therefore reached the one address `release.yml` built it with,
`http://garam-api:8080`, which is the api Service a release named
`garam` renders, and under any other release name the rewrite reached a
name that resolved to nothing and no render or probe said so. ADR-0056
kept that, rejected a runtime value because it meant code of this
project's own in front of every browser call, and added `chart-rewrite`
to hold the baked value to the chart's Service name.

Three things changed what that costs, each read on 2026-11-13:

- **The proxy carries the rewrite.** In Next.js 16.3.1 a `proxy.ts`
  that returns `NextResponse.rewrite` to an external URL goes through
  the same proxy path as a config rewrite, evaluated when the request
  arrives. Its `env` is read when the process starts. This is the
  framework's own mechanism, not one this project writes, which is what
  ADR-0056's alternative assumed it would be.
- **Streaming and bodies pass.** Run against two stub backends,
  `make web-image-e2e` shows a streamed response's first chunk
  reaching the client more than two seconds before its second, and a
  4 MiB request body arriving whole; the framework buffers a request
  body up to 10 MB and the backend refuses one over 1 MiB, so nothing a
  caller can send reaches the backend differently.
- **The release is to publish `garam/policy`.** A release builds and
  pushes six images, so the web images' constraint was the one thing
  tying a published image to a deployment's naming.

`docs/convention/stack-nextjs.md` §11 gives a `proxy.ts` matcher that
selects "everything except `/api`" and calls a general middleware hub an
anti-pattern; §6 keeps Route Handlers for external callers. The matcher
here is a list of the paths the proxy has a job on, and `/api/:path*` is
one of them for its rewrite alone. The reading of §11 this rests on is
reported in the pull request rather than decided here.

## Decision

- **The `/api/*` rewrite is made in each web app's `src/proxy.ts`, from
  `GARAM_API_URL`, and no image holds an address.** `platform-web` and
  `site-web` drop the `rewrites()` block of `next.config.ts`. The proxy
  answers `/api` and `/api/*` by sending the request to
  `GARAM_API_URL` with the `/api` segment dropped, the query kept, and
  the path as it was sent, a percent-encoded segment included.
  `deploy/local/Dockerfile.web` takes no `GARAM_API_URL` build argument
  and `release.yml` passes none.
- **A published web image is bound to no release name.** A deployment
  sets `web.apiUrl` and `site.apiUrl` to the api Service its release
  renders. ADR-0056's "a deployment installs under the release name
  `garam`, or makes `garam-api` resolve" no longer applies.
- **With `GARAM_API_URL` unset, every request answers 500, the probes
  included.** The production schema in `env.ts` has no default and fails
  to parse when the proxy module loads, which is the first request. The
  process stays up and its readiness probe fails, so the pod is held out
  of service rather than reaching a default address. Before this change
  the variable was required in production as well, at `next build` and
  at import, and an unset run served its probes and answered the server
  rendered routes with 500; this makes a probe answer with the routes
  rather than apart from them, which is the failure the readiness probe
  is there to report. [ADR-0023](./0023-web-probes.md)'s statement
  that a probe reads nothing outside the process holds for a configured
  process; an unconfigured one is not ready.
- **`chart-rewrite` is removed.** It existed only to hold the value a
  release baked to the api Service a release named `garam` renders, and
  nothing is baked. `chart-names` keeps its release name and every other
  target in `make -C deploy/charts ci` is unchanged. What replaces it is
  `make web-image-e2e` and `make site-image-e2e`, which run one image
  against two backends and search it for an address.
- **A release publishes six images,** `garam/policy` among them, all or
  none, and `make image-build` builds the six through
  `tooling/image-build.sh`.

## Consequences

- A deployment names its api Service in a value and any release name
  works; a wrong value fails as a console that reaches nothing, as a
  wrong `web.apiUrl` already did for every server-side render.
- The image is smaller in what it asserts: nothing there ties it to a
  cluster object, so no check in this repository reads such a tie.
- The project owns two proxy branches and their tests. The rewrite goes
  through Next's proxy, so cookies, `Set-Cookie`, streaming and the
  30-second proxy timeout are the framework's, not this project's.
- `v0.1.0` and every release before the next keep the address they were
  built with. The tags are immutable, so a deployment on one still needs
  the release name `garam` until it moves to a version built after this.
- ADR-0039's and ADR-0134's statements about the address are history;
  the web-app responsibility document and the chart README state the
  present.

## Signs the decision was wrong

- A request reaching the backend with a path or body other than the one
  the browser sent, such as a `%2F` decoded into segments, a body
  truncated, or a response held to its end.
- An `/api/*` call answered by the framework's own 404 or 500 where the
  backend would have answered, in a deployment that sets the address.
- A Next.js upgrade that evaluates a proxy rewrite's destination at
  build time, or stops streaming through it; `make web-image-e2e` is
  where either shows.
- A deployment that wanted the image bound to a fixed name for the
  safety of it, and a missing `web.apiUrl` that no one noticed because
  its pod was Ready; the 500 on the probes is meant to prevent that.
