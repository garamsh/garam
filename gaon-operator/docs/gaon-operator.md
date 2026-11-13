# gaon-operator

The operator that runs each managed agent as one MicroVM on a gaon
cluster
([ADR-0151](../../docs/architecture/adr/0151-the-gaon-operator-is-a-service-here-that-runs-each-managed-agent-as-one-microvm.md)).
How it sits among garam's operators is
[operators.md](../../docs/architecture/operators.md) §The gaon operator.

**Built: the reconciler (#1834), garam's and the control service's wire
clients, the binary that composes them (#1839), the guest supervisor
and composed guest image (#1846), and the MicroVM's command,
environment and placement token (#1848).**
`cmd/gaon-operator` exchanges its assertion with a key garam issued,
reads the control service's feed as its registered manager and
reconciles into gaon. The release's image and the deployment are not
built (§What is not built).

## Contents
- What is built
- What is not built
- What the tests prove
- Rationale
- Open questions

## What is built

`internal/agent` holds the reconciler. `Service.Reconcile` is one
level-triggered pass: it reads the whole feed, lists the objects it
labelled in gaon, and writes what differs. It keeps no state a restart
loses.

- **Objects.** An agent yields one `Secret`, two `Volume`s (memory and
  workspace) and one `MicroVM`, named `agent-` plus 16 hex digits of a
  SHA-256 of the agent's GRN (`-memory`, `-workspace` on the Volumes).
  Labels are `garam.sh/role=agent` and 32-hex digests of the
  organization and agent; the GRN, epoch and revision are annotations,
  because a label value cannot hold a GRN.
- **No credential in a spec.** The tokens, the placement token and the
  two trust roots live in the `Secret`. The MicroVM's files take them
  with `valueFrom.secretKeyRef`, mode 0400, owner 65532 (the runtime's
  and adapter's account; the MicroVM's `user` is `0:0`); no file
  carries inline content, and no `env` entry does either where it is a
  secret.
- **Command and environment.** `command` is
  `["/usr/local/bin/gaon-guest"]` and `args` is empty: gaon's
  `command` replaces the image's entrypoint, and the supervisor builds
  each program's arguments itself. `env` holds `GUEST_AGENT_GRN` and
  `GUEST_ASSIGNMENT_EPOCH` from the feed, `GUEST_MACHINE_URL` and
  `GUEST_CONTROL_URL` from the settings, and, where the model key is
  set, `GUEST_RUNTIME_ENV` naming the variable and that variable as a
  `valueFrom.secretKeyRef`.
  `GUEST_RUNTIME_CONFIG` is not rendered, because the feed's
  `configuration` is not rendered by the operator.
- **Volumes are made once.** They are created only where the agent has
  no MicroVM yet. Where the MicroVM exists and a Volume is missing, the
  operator creates nothing for that agent, returns an error naming the
  Volume and reports no state: a Volume is removed only by a person
  (ADR-0151 §3).
- **Mint once.** A Secret that exists is never minted over. One that is
  missing is minted again, which is what makes a restart lose nothing.
  The placement token is minted with it: 32 bytes from `crypto/rand`,
  hex-encoded, under the key `placement-token`, as
  garam-agent-operator's ADR-0042 mints it. A Secret that exists keeps
  what it holds, so one made before this change is not given a token.
- **Report.** `observe` maps a MicroVM to `provisioned`, `ready` or
  `failed` by the ADR's table, reading the phase through
  `EffectivePhase` so a status of another `bindingEpoch` counts as
  pending. `failed` needs a last exit. Nothing is reported where nothing
  was read, and each change is reported once. A stale epoch refused by
  garam is dropped and logged.
- **Halt, never delete.** A `stopped` agent, and one that left the feed,
  has its MicroVM's `runStrategy` set to `Halted`. Nothing is deleted:
  the Volumes are the agent's memory.
- **Settings.** `internal/config` reads addresses and file paths from
  the environment and names each missing variable, never a value.

gaon's side is `lib/api` at `github.com/garamsh/gaon` commit
`4d950576aa75baab497f327e0b3bb5381488c740`.

### The wire clients and the binary

- **Exchange** (`internal/token`). A `Source` signs a `private_key_jwt`
  (RS256, `typ` JWT, `kid` the key's RFC 7638 thumbprint, `iss` and
  `sub` the client id, `aud` the token URL byte for byte, `iat` and
  `exp` 60 seconds apart, a fresh `jti`), exchanges it at garam's token
  endpoint and holds the token in memory until a minute before it
  lapses. The key is read again at each exchange. Two Sources exist:
  garam's own, and one that asks for `audience` the control service's
  GRN, which only the control service accepts.
- **Garam** (`internal/agent/machineapi`). `MintTokens` mints two
  tokens under contract `machine-token.v1`: the agent's own and the
  control token, minted with the control service as audience. A token
  minted at an epoch other than the feed's, and a 409
  `failed_precondition` from `reportProvisioningState`, are
  `agent.ErrStaleEpoch`. A 401 drops the held token and asks once more.
- **Feed** (`internal/agent/controlapi`). `GET
  /v1/operators/self/desired?after=&waitSeconds=` under the control
  token and `Controller-Grn`; the status report is `POST
  /v1/operators/self/agents/{grn}/status`. The cursor moves only when
  the loop settles it after a pass that succeeded, so a failed pass
  asks again at once. A profile without `workspaceStorageSize` gives
  the workspace Volume the size of the memory Volume.
- **An answer that cannot be read whole is an error, never an empty
  set**, because an empty set halts every agent. These all fail the
  pass: a status other than 200; a body cut short or longer than 16 MiB;
  invalid JSON, trailing data, an unknown field, a missing cursor, a
  null or absent `agents`; an agent without a required field, with a
  GRN that is not a canonical agent GRN, with a `revision` or `epoch`
  that is not a canonical decimal string, with a bad quantity, or named
  twice. Only `{"cursor": "...", "agents": []}` is an empty set.
- **Loop and health** (`internal/loop`, `internal/health`). Passes
  follow one another, the long poll pacing them; a failed pass is
  retried after 1 second doubling to 1 minute. `/healthz` is 200 while
  the process runs and `/readyz` while the last pass succeeded. SIGINT
  and SIGTERM stop the loop and the listener.

### Settings

Addresses and paths, in the environment; none holds a credential.

| Variable | Meaning |
|---|---|
| `GAON_API_URL`, `GAON_CA_FILE`, `GAON_CLIENT_CERT_FILE`, `GAON_CLIENT_KEY_FILE` | gaon's API, its CA, and the operator's client certificate and key |
| `GARAM_MACHINE_URL`, `GARAM_ROOT_FILE` | garam's machine listener and its root |
| `GARAM_TOKEN_URL` | the token URL the key issuance answered, the assertion's `aud` |
| `CONTROL_URL`, `CONTROL_ROOT_FILE`, `CONTROL_GRN` | the control service, its root, and its operator GRN |
| `OPERATOR_KEY_FILE`, `OPERATOR_CLIENT_ID` | the operator's key, and the manager's operator GRN, which is also the `Controller-Grn` |
| `AGENT_IMAGE` | the composed guest image |
| `GUEST_MACHINE_URL`, `GUEST_CONTROL_URL` | optional: the addresses the adapter in the guest reaches garam and the control service at; each defaults to `GARAM_MACHINE_URL` and `CONTROL_URL` |
| `MODEL_KEY_ENV`, `MODEL_KEY_SECRET`, `MODEL_KEY_SECRET_KEY` | optional, all three or none: the variable the runtime reads its model key from, and the Secret and key holding it |
| `HEALTH_ADDR` | optional, default `:8081` |

### Key files

The module cannot import `platform/internal/keyfile`, so
`internal/keyfile` applies its rule (ADR-0015): a key file readable by
any group or other user, or owned by another user, is refused, with the
mode and owner read from the open descriptor. It holds the operator's
key and gaon's client key; the operator stops at start naming the path
and never the contents. The trust roots are certificates and are read
plainly.

### The guest supervisor and image

`cmd/gaon-guest` is the entrypoint of the composed guest image
(ADR-0151 §3, its user by ADR-0153). `internal/guest` holds it, and the
constants the operator's MicroVM spec and the supervisor both name (the
accounts, the credential, memory and workspace paths, port 8088), so
each is written once.

- **Accounts.** The MicroVM's `user` is `0:0`, so the supervisor is
  root. It starts the runtime and the adapter as 65532:65532 with no
  supplementary group and so no capability, and the workspace as root
  through `setpriv --bounding-set` keeping the standalone runner's five
  (`SETUID`, `SETGID`, `CHOWN`, `SYS_ADMIN`, `KILL`) with
  `SHERLOCK_EXEC_UID=10001`. Before it starts anything it makes the
  memory directory and the outbox 0700 and 65532's, refusing a path that
  is a symbolic link. The credential files are 65532's at 0400, which
  the operator's `files` entries state as owner. The model credential is
  named by `GUEST_RUNTIME_ENV` and reaches the runtime alone.
- **Order.** `Run` starts the runtime, the workspace and the adapter in
  that order, opens the readiness port 8088 only once all three are
  started, and on SIGTERM or the first program's exit sends SIGTERM to
  each still-running program in start order, waiting for each before
  the next, as garam's standalone runner does.
- **Settings.** The environment names `GUEST_AGENT_GRN` (a canonical
  agent GRN), `GUEST_ASSIGNMENT_EPOCH` (a canonical decimal),
  `GUEST_MACHINE_URL`, `GUEST_CONTROL_URL` and optionally
  `GUEST_RUNTIME_CONFIG` and `GUEST_RUNTIME_ENV`. A program gets only
  `PATH`, `SSL_CERT_FILE` and `SHERLOCK_TOOLS_DIR` from it, plus what
  its command needs.
- **The image** (`guest/Dockerfile`) is the workspace image with the
  runtime's tools and CA bundle, garam's binary and `gaon-guest` copied
  in, and no `USER`. The three source images are pinned by digest as
  `ARG`s and recorded as `sh.garam.guest.*-image` labels. The build
  stops if the runtime image's `/sherlock` and the workspace image's
  differ. The pins are Sherlock 0.10.0 for the two sherlock images and
  garam 0.7.0 for the server; `platform/Makefile` pins Sherlock 0.9.0
  for the runner's tests, so these digests are pinned nowhere else in
  the repository.
- **Running it.** `make -C gaon-operator guest-run` builds the image
  and starts it as one container with user `0:0` and `SYS_ADMIN` added.
  The entrypoint, the environment and the credential files are what
  `render.go` renders for one agent, written out by `cmd/gaon-render`
  through `agent.Render` and not typed in `guest/run.sh`: it resolves
  each `valueFrom` from the rendered Secret, or from a file standing for
  the deployment's model Secret, and stops on a source it cannot
  resolve, on a command of more than one word and on an argument. It
  checks the start order, the
  readiness port and the SIGTERM drain, each program's user and
  capabilities, that the model credential is in the runtime's
  environment alone, and that a command run as 10001 is refused the
  store, the outbox, each credential file and the runtime's and the
  adapter's environment while 65532 reads the same. It needs Docker, a
  registry login and openssl and is not part of `ci`.

The guest still does not isolate the workspace process from the store:
it is root in the same guest and holds `CHOWN` and `SETUID`, so it could
take the store or become 65532. The runtime and the adapter share one
account, so the runtime can read the adapter's tokens. What gaon's guest
kernel admits for the workspace executor (the user and network
namespace clone, refused under Docker's default seccomp) is not known.

Open in the guest: the supervisor does not reap orphaned Exec children
that reparent to it.

Tokens: `agent-token` is the adapter's agent token and `control-token`
is the agent token minted with the control service as audience, which
the adapter presents to the lifecycle authority (adapter.md §Token
mode). The placement token is separate, sent in `Garam-Placement`. The
operator mints it and delivers it as the file `placement-token`, as
above.

### What the deployment must create

- **The model Secret**, where `MODEL_KEY_*` is set: a gaon `Secret`
  named by `MODEL_KEY_SECRET` holding the key `MODEL_KEY_SECRET_KEY`.
  The operator never reads or logs it and creates none; gaon resolves
  the `valueFrom` when the MicroVM starts. It is one Secret for every
  agent the operator runs.
- **The trust roots and the key files** the operator itself reads
  (§Settings).

### What is not established

- **Registering a placement.** A placement must be registered, or the
  adapter's activation is refused with 403 `placement_not_current`
  (garam-agent-operator `docs/architecture/control.md` line 188). The
  control service serves the route, `POST
  /v1/operators/self/agents/{agent}/placements` (`control.md` lines
  163-181; `internal/distribution/server.go` line 54), with the body
  `{epoch, podUid, pvcUid, tokenSha256, previous}`, all at commit
  `ac7c9f3c40b9611f6e2bdf1eba62c14e145b4aec`. This operator does not
  call it: `podUid` and `pvcUid` are a Pod's and a claim's, and
  `previous.writerStoppedSha256` digests a Pod's `status.writerStopped`
  (`control.md` line 166). A MicroVM and a Volume are not those, and
  which of their fields stand for them, and what evidence a MicroVM's
  replacement writes, is a decision ADR-0153 leaves open. Until it is
  made, an agent this operator renders cannot activate.
- **A model key per agent.** The feed's `configuration.model.apiKeyRef`
  is a per-agent `<secret-name>/<key>` (`control.md` line 113,
  `agent.md` line 16 of that repository), which this operator parses
  nowhere because it does not read `configuration`, and the variable
  name sherlock reads the key from is written in the config file's
  `model.api-key-env` (`agent.md` line 136), which this operator does
  not render. The setting above therefore gives every agent one key. An
  agent configured with another reference is run with this one.

## What is not built

- The image `garam/gaon-operator` and its deployment, and the
  `NetworkPolicy`.
- Renewing an agent's token by restarting its MicroVM.
- Replacing a MicroVM of another revision.

## What the tests prove

They run the reconciler against stand-ins for the feed and for garam,
and the wire clients against `tests/garamfake`, an `httptest` server of
the machine listener's three operations that checks an assertion as
ADR-0151 §6 says garam does, and against `tests/gaonfake`, an
`httptest` server of gaon's REST paths built on `lib/api`'s types and
validation. The fake enforces
compare-and-swap on `resourceVersion`, the API's validation and
label-selector lists. It does not prove: RBAC selector rules, garbage
collection, the Node authorizer, a get refused for lack of labels,
`bindingEpoch` as gaon writes it, or watches.

The supervisor's tests start helper processes and prove the start
order, the readiness port opening after all three start, the drain
order with a wait for each, the drain after one program exits early,
the settings and account each program receives, and a store path that
is a symbolic link refused. `guest-run` shows in the built image each
program's user and capabilities and a command run as 10001 refused,
started from what `render.go` renders.
They do not prove the adapter's calls to garam, which need an admitted
agent, an Exec under the guest kernel, or gaon's MicroVM start.

## Rationale

The reconciler declares its two outside dependencies; the wire clients
fill those interfaces. It deletes nothing because nothing in the feed
says an agent is gone for good.

## Open questions

- Whether the MicroVM should carry an owner reference to its Secret and
  Volumes, which waits on gaon's garbage collector being verified.
