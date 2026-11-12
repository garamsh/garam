# gaon-operator

The operator that runs each managed agent as one MicroVM on a gaon
cluster
([ADR-0151](../../docs/architecture/adr/0151-the-gaon-operator-is-a-service-here-that-runs-each-managed-agent-as-one-microvm.md)).
How it sits among garam's operators is
[operators.md](../../docs/architecture/operators.md) §The gaon operator.

**Built: the reconciler (#1834), garam's and the control service's wire
clients, and the binary that composes them (#1839).**
`cmd/gaon-operator` exchanges its assertion with a key garam issued,
reads the control service's feed as its registered manager and
reconciles into gaon. The guest image, the release's image and the
deployment are not built (§What is not built).

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
- **No credential in a spec.** The tokens and the two trust roots live
  in the `Secret`. The MicroVM's files take them with
  `valueFrom.secretKeyRef`, mode 0400, owner 65532; no file carries
  inline content.
- **Volumes are made once.** They are created only where the agent has
  no MicroVM yet. Where the MicroVM exists and a Volume is missing, the
  operator creates nothing for that agent, returns an error naming the
  Volume and reports no state: a Volume is removed only by a person
  (ADR-0151 §3).
- **Mint once.** A Secret that exists is never minted over. One that is
  missing is minted again, which is what makes a restart lose nothing.
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
  tokens under contract `machine-token.v1`: the agent's own and a
  placement token with the control service as audience. A token minted
  at an epoch other than the feed's, and a 409 `failed_precondition`
  from `reportProvisioningState`, are `agent.ErrStaleEpoch`. A 401
  drops the held token and asks once more.
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
| `HEALTH_ADDR` | optional, default `:8081` |

### Key files

The module cannot import `platform/internal/keyfile`, so
`internal/keyfile` applies its rule (ADR-0015): a key file readable by
any group or other user, or owned by another user, is refused, with the
mode and owner read from the open descriptor. It holds the operator's
key and gaon's client key; the operator stops at start naming the path
and never the contents. The trust roots are certificates and are read
plainly.

## What is not built

- The guest supervisor and the composed guest image.
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

## Rationale

The reconciler declares its two outside dependencies; the wire clients
fill those interfaces. It deletes nothing because nothing in the feed
says an agent is gone for good.

## Open questions

- Whether the MicroVM should carry an owner reference to its Secret and
  Volumes, which waits on gaon's garbage collector being verified.
