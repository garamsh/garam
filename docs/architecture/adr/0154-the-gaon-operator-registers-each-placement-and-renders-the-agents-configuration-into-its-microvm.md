# ADR 0154: The gaon operator registers each placement and renders the agent's configuration into its MicroVM

> Status: accepted; built in `gaon-operator/`
> Date: 2026-11-13

Drafted for #1853. It closes the open dependency
[ADR-0153](./0153-the-gaon-guest-runs-as-root-and-its-supervisor-drops-each-program-to-the-account-it-needs.md)
names, where nothing registers a placement, and settles the model key
that ADR-0151 left as three settings for every agent. It supersedes no
decision: ADR-0153's status stays as it is.

## Context

An agent the operator starts cannot activate. The adapter presents the
placement token on activation, and the lifecycle authority refuses it
with 403 `placement_not_current` until the control service holds a
placement that names that token's digest. The control service serves
the route, `POST /v1/operators/self/agents/{agent}/placements`, with a
body of `epoch`, `podUid`, `pvcUid`, `tokenSha256` and `previous`
(`garamsh/garam-agent-operator` `docs/architecture/control.md`, read at
commit `ac7c9f3c40b9611f6e2bdf1eba62c14e145b4aec`). The Kubernetes
operator fills `podUid` and `pvcUid` from a Pod and a claim. A MicroVM
and a Volume are not those, so which of their fields stand for them was
undecided. #1853 decides it.

The runtime also has no configuration. The feed carries a
`configuration` (model, embedding, tools, ego) that the operator did not
read, and the model key came from three settings, `MODEL_KEY_ENV`,
`MODEL_KEY_SECRET` and `MODEL_KEY_SECRET_KEY`, so every agent ran with
one key whatever its `apiKeyRef` said.

The forces on where the configuration goes:

- A Node can read every MicroVM spec, so nothing secret may be inline.
  The configuration holds no secret: the key is named by a variable and
  reaches the runtime as an environment entry resolved from a Secret.
- gaon resolves a `valueFrom` once, when the MicroVM starts, and a
  MicroVM's spec is immutable apart from `runStrategy`,
  `idleTimeoutSeconds`, `resources` and `volumes`. A changed
  configuration therefore reaches a running MicroVM by neither route.
- Inline file content is bounded at 64 files and 1 MiB in all. A
  configuration and an ego are far inside it.
- The tool pins are in the configuration, and the executor's child,
  10001, must not read them.

## Decision

**A placement is registered with the control service by the operator,
once the MicroVM exists.** The body is made from what the operator
holds:

- `podUid` is the MicroVM's `metadata.uid`, which gaon assigns, so a
  first placement is registered after the MicroVM is created;
- `pvcUid` is the memory Volume's `metadata.uid`;
- `tokenSha256` is the SHA-256 of the placement token as minted, read
  from the agent's Secret, and the token itself is never sent;
- `epoch` is the feed's assignment epoch as a decimal string;
- `previous` is null.

The operator sends the control token and `Controller-Grn` as the feed
does, and reads the answer as the contract gives it:

- 201 is created and 200 an identical repeat, and neither is an error;
- a 409 `placement_superseded` or `epoch_superseded` is final: that body
  is never presented again, and a new body (another epoch, token or
  uid) is registered as a first one;
- any other 4xx, among them 409 `previous_mismatch`,
  `placement_conflict` and `evidence_missing`, and 403, is presented
  again after five minutes;
- a 5xx or a transport error fails the pass, which the loop retries
  with its backoff.

A stopped agent is not registered. An agent whose Secret has no
placement token, or whose MicroVM or memory Volume has no uid, fails the
pass and is retried.

**`previous` and `writerStoppedSha256` are not built.** The digest the
issue maps is of the canonical JSON (RFC 8785) of the stop observed on
the earlier MicroVM, its phase and `lastExit`. Only a first placement
registers here, and a replacement of a MicroVM is not done.

**The runtime's configuration is two inline files and the key is an
environment entry.** From the feed's `configuration` the operator
renders `/run/garam/runtime-config.yaml` (the model's provider,
endpoint, name and `api-key-env`, the embedding's endpoint and name,
and `tools.pins`) and `/run/garam/runtime-ego.md`, as `files` entries
with `content`, mode 0400, owner 65532. `GUEST_RUNTIME_CONFIG` and
`GUEST_RUNTIME_EGO` name them, and the supervisor gives the runtime
`--config` and `--ego-file`. Inline content, not a `ConfigMap`, is the
choice: it holds no secret, 65532 alone reads it, so 10001 cannot read
the pins, and it adds no object per agent to create, label and
reconcile. Each agent's content is its own, so two agents with two
references render apart.

**The model key is an `env` entry with `valueFrom.secretKeyRef`.** The
Secret and key are those `apiKeyRef` names as `<secret-name>/<key>`, and
the variable is the one the configuration names, `MODEL_API_KEY`, and
`EMBEDDING_API_KEY` for an embedding that needs one. The operator never
reads the value. `GUEST_RUNTIME_ENV` names the variables, so they reach
the runtime alone. A name matching `agent-` and 16 hex digits is
refused, as it is one of the operator's own Secrets. A configuration the
operator cannot render writes nothing for the agent and fails the pass.

**A reference is honoured only for a model-key Secret of the agent's
organization.** gaon has no namespaces and a Node hands a MicroVM any
Secret its spec names, so an organization could otherwise name another's
Secret, garam's database URL or its key-encryption key, and the value
would land in its agent's environment. The Secret must exist when the
MicroVM is rendered and carry both `garam.sh/role=model-key` and
`garam.sh/organization=<digest>`, where the digest is the label the
operator puts on that agent's own objects: the first 32 hex digits of
the SHA-256 of the organization's GRN string (`grn:org:<org>`), since a
label value holds 63 characters. Any other reference, absent,
unlabelled, another organization's or the operator's own, starts nothing
for that agent and fails its pass with an error naming the reference and
no value. The operator finds the Secrets by label with a second gaon
client whose selector is the role alone, because a client's selector is
fixed and the organization differs by agent; it filters by organization
in code, once per pass and only where an agent about to be created names
a model. Where `Options.ModelKeys` is nil no reference is honoured.

The list returns each Secret's `data` to the operator. It drops the data
on arrival and neither uses nor logs it; the listing is a read of the
key's value that the operator's gaon role is therefore trusted not to
misuse. The role needs read on Secrets labelled
`garam.sh/role=model-key` and on no other Secret beyond its own objects.
The deployment creates each organization's model-key Secret with both
labels; the operator creates none.

**`MODEL_KEY_ENV`, `MODEL_KEY_SECRET` and `MODEL_KEY_SECRET_KEY` are
removed.**

**Two profile fields mean something else in a guest, and nothing is
invented for them.** `profile.storageClassName` is read and not used:
gaon's Volume has the one class the operator always names
(`api.StorageClassLocal`). `profile.resources` is read and not used:
`Options.Resources` sets the MicroVM's, one value for every agent.
They are parsed in `controlapi/wire.go`, and the Volume and MicroVM
are made in `agent/render.go` and `agent/service.go`.

## Consequences

- An agent the operator starts can activate, once the deployment has
  created the Secret each `apiKeyRef` names. gaon resolves it at the
  MicroVM's start, so a Secret created later is not seen by a MicroVM
  that already started.
- An agent whose `apiKeyRef` names another Secret is run with that one,
  and the setting that gave every agent one key is gone. A reference to
  a Secret the deployment has not labelled for the organization starts
  nothing for that agent, and the pass fails until it is labelled.
- The operator's gaon role reads model-key Secrets, and so reads their
  values. A Secret created or labelled after a MicroVM started is not
  seen by it, and that the named key exists inside the Secret is not
  checked.
- A change of an agent's configuration does not reach its MicroVM: the
  spec is immutable and the `valueFrom` is not refreshed. It takes a
  MicroVM of another revision, which is not built.
- The configuration's content is in the MicroVM spec, which a Node
  reads. It holds the model's endpoint and the tool pins and no key.
- The agent's instructions file and message issuers are not rendered.
  The runtime runs without them.
- The registration reads the placement token back from the listed
  Secret. That assumes gaon returns a Secret's `data` on a list, which
  the fake does and a real gaon was not run for.
- A Secret that exists without a placement token fails its agent's pass
  for good, with the loop's backoff. The operator never mints over a
  Secret, so a person deletes that Secret and the next pass mints it
  again; nothing else recovers it.
- The token restart, the release image and the `NetworkPolicy` stay
  unbuilt, and so does a cluster run.

## Signs the decision was wrong

- A listing of model-key Secrets is refused to the operator's gaon
  identity, or gaon offers a list that leaves out `data`, which would
  make the second client's reach smaller than this ADR states.
- gaon does not return a Secret's `data` to the operator, so the digest
  cannot be made from what the operator reads.
- The control service refuses a MicroVM's uid as a `podUid` or the
  memory Volume's as a `pvcUid` on a real cluster, with
  `evidence_missing` or `placement_conflict` where the body is right.
- A configuration or ego outgrows the inline bound of 1 MiB for the
  files of one MicroVM, or a Node-readable spec becomes unacceptable for
  the pins.
- gaon begins to refresh a `valueFrom` or to let a spec change, so that
  a configuration could reach a running MicroVM and this ADR's
  consequence about revisions is the defect.
- A replacement of a MicroVM is needed before `previous` is built, and
  an agent stays unable to activate after it.
