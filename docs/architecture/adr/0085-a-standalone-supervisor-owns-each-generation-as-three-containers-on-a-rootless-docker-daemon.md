# ADR 0085: A standalone supervisor owns each generation as three containers on a rootless Docker daemon

> Status: accepted
> Date: 2026-10-05

Drafted for #1180, which reserved this number. The arrangement is the
garam PM's decision of 2026-10-05 on #1180 (comment 5987091349), the
runtime pin is the comment after it (5988125091), and the transport,
the placement token, the run command and the store rule are the PM's
answers of the same day to the worker's proposal on the dispatch. The
design evidence is the read-only preflight that decision disposed of.
Facts about Sherlock hold at `v0.2.0`
(`b3c05c24bc2a346521251556935bfe0dc70f8cd1`); facts about Docker hold
for Engine 29.8.1 run rootless, as measured on the host the tests ran
on.

## Context

ADR-0082 gave a standalone host a supervisor garam registers for one
agent, and left how it runs that agent's runtime to #1180. ADR-0084
gave the adapter one client for activation and runtime status, and
left the transport between a standalone supervisor and its adapter to
#1180 too. Four gaps stood between them:

- Sherlock's gateway refuses a non-loopback address without TLS, so a
  published port does not reach it.
- An engine's ordinary stop kills after a timeout, which would turn a
  stuck drain into a killed writer.
- A volume has no identity beyond its name, and a container mount
  creates one under a name where none exists.
- A create whose answer is lost leaves a container the supervisor has
  no record of, and a label anyone with the engine socket can write
  proves nothing.

## Decision

**One arrangement: a host process that alone holds the Docker socket
and the supervisor's credential owns each generation as three
containers sharing one network namespace.** `garam standalone run`
runs in the foreground over one rootless daemon. It returns once the
generation it supervises has exited and been drained; restarting it is
the host's init's job, and a supervisor that stops leaves the workload
running.

- **The runtime** runs the pinned `sherlock-agent` image as 65532 with
  every capability dropped and no new privileges, owning the
  namespace. Its gateway stays on loopback and no port is published.
  It reads only its configuration and the model credential.
- **The workspace** runs the pinned `sherlock-workspace` image with the
  five capabilities its executor needs, joined to the runtime's
  namespace, so file and exec tools work.
- **The adapter** runs this release's garam image as 65532, the
  account that writes the outbox, with every capability dropped,
  joined to the same namespace. It reaches the runtime on loopback
  exactly as a managed pod's sidecar does.
- **Every image is named by a digest or an image ID**, and every
  container is created with no restart policy, no published port, no
  host namespace and a stop timeout of -1.

**Storage is two volumes the supervisor created.** The memory volume
is mounted into the runtime whole, and into the adapter as its
`outbox` directory alone, by the engine's volume subpath. The
workspace volume is mounted into the workspace alone, so no Exec child
reaches memory: sherlock#966's invariant and garam-agent-operator#249's
decision hold here too, and the adapter, whose account owns the store,
cannot open it.

- The supervisor records each volume's name and a random nonce before
  it creates the volume with the nonce as a label. It confirms the
  label before every start and again after each container is created,
  because a mount creates a missing volume.
- A confirmed volume that is gone, or one that carries another nonce,
  is refused. The supervisor creates nothing on it and waits for a
  person; it never creates a volume to replace it.
- The supervisor makes the outbox, a directory owned by 65532, by
  copying it into the created runtime before its first start, and
  checks it again through the runtime before the adapter is created.
  Anything there but a directory refuses, and the generation waits for
  a person; the path is never repaired, since replacing it could drop
  what it holds.
- That check is sufficient because of when it runs. On the rootless
  daemon measured, a subpath that is a link inside the volume
  (`outbox -> .`) is followed, and mounts the whole volume; one that
  leaves the volume is refused. A tool runs as 65532, which owns the
  volume, so a tool of an earlier generation could plant such a link.
  Between the check and the adapter's mount no turn can run, because a
  turn begins only with a message the adapter delivers, so nothing
  changes the path in between. A link planted after the adapter starts
  does not move a mount already made. Entries inside the outbox are
  still read through the adapter's own file boundary (ADR-0084).
- The runtime starts with `--require-store` on every start except the
  first ones on a new memory volume. Which those are is a durable fact
  in the volume's record, written when the first runtime-status report
  saying `serving` arrives for a runtime on it. Nothing infers it, an
  activation included, so a runtime gone between its activation and
  its first report leaves the next start creating the store, and a
  supervisor restarted after a serving report always passes the flag.

**A generation is an intent recorded before anything of it exists.**
The intent carries:

- an identifier, from which every container name is derived;
- a random nonce, written as a label on each container;
- the images, the epoch and the revision it runs;
- the placement token.

The record is written first, and each container's ID is written when
the engine answers. A container under a derived name is adopted only
when it carries the intent's nonce and identifier, its image and its
volumes. That recovers a create whose answer was lost without creating
a second. Anything else under the name is foreign: it is never started,
signalled or removed, and the run stays pending. A label alone adopts
nothing. Once the record holds a container's ID, that ID alone finds
it, so a rename neither hides a container from the drain nor puts a
stranger in its place.

**A drain is SIGTERM and a wait with no deadline, one container at a
time.** The supervisor sends SIGTERM to the runtime and waits for the
engine to report it exited, so a turn it is finishing keeps the tools
it calls. It then does the same for the workspace, then for the
adapter, and only after that removes the three. A runtime finishing a
turn keeps the drain pending for as long as that takes, and so does a
recorded container the engine cannot account for. No stop with a
timeout is used, and nothing escalates.

- The positive exit is both writers exited. When a namespace's first
  process exits, the kernel ends every process in that PID namespace,
  so a tool that detached keeps nothing running past it.
- Any of the three exiting on its own ends the generation, and the
  other two are drained the same way. So does the generation's control
  listener stopping on its own, which would leave the adapter no way
  to activate.
- A replacement happens when the desired revision, epoch or images
  differ from the running generation's, or when `--replace` asks for
  one. It drains the old generation before the new one is created.

**Credentials are copied into a container, never bind-mounted.** The
supervisor uploads each file in an archive the engine extracts
keeping its owner, before the container starts. Under a rootless
daemon a host file shows up as container root, which the adapter's own
key check refuses.

- The runtime receives its configuration, owned by 65532 and mode
  0600.
- The adapter receives the agent's key and certificate, the garam
  server root, the generation's control root and the placement token,
  each owned by 65532 and mode 0600.
- The runtime's environment is the model credential and nothing garam
  holds.

**The adapter reaches the supervisor on a unix socket of its
generation, over TLS, under ADR-0084's contract unchanged.**

- The socket sits in a directory of the supervisor's own that is
  mounted into that generation's adapter and nothing else.
- The supervisor serves TLS 1.3 there with a certificate for
  `supervisor`, signed by a root minted for the generation whose key
  is discarded once it has signed. The root dies with its generation:
  the drain removes the directory that holds it.
- The adapter presents the agent's certificate, which the supervisor
  verifies against the agent's issuer and requires to name the GRN in
  the path. That presented leaf is what it forwards to garam as
  `certificatePem`.
- `GARAM_ADAPTER_CONTROL_SOCKET` makes every control connection dial
  the socket while TLS still verifies `GARAM_ADAPTER_CONTROL_URL`'s
  host. It is valid only beside the three settings that select the
  fenced mode.

**Activation is forwarded for the generation this intent started and
for nothing else.** The supervisor requires each of these before it
forwards:

- the intent's placement token, 32 random bytes compared in constant
  time; a request without one is unauthenticated;
- the epoch and the configuration's own `revision`, which `run`
  refuses to be empty;
- the runtime container being the recorded one, running from the
  recorded start with no restart since;
- a generation never asked for before on this host.

The generation, the request identifier and the anchor the first
request named are recorded before garam is called, and every retry
repeats them, so garam answers the same activation with a newer token
and never a second one. The supervisor maps garam's answers this way:

- unavailable, or garam refusing the supervisor's own certificate, is
  `503 undecided`;
- garam's 409 is `activation_superseded`;
- any other refusal is `not_authorized`.

A runtime status is accepted for the activated generation alone and is
the host's record of what the runtime applied.

**The adapter activates only a runtime that reports `serving`.** This
refines ADR-0084's "reads the runtime's status ... and activates" for
the fenced mode: a runtime already told to stop reports `draining`, and
its generation is not the one to fence the next by. A runtime that
drains after its activation keeps it, and its status is reported as
before.

**An engine the supervisor cannot reach decides nothing.** It starts,
signals, removes and activates nothing, and the generation keeps
running with the token its adapter holds. Stopping the daemon may stop
the runtime, and recovery after that is a new generation; that is a
limit and not a promise.

## What this does not decide

- **Joint acceptance with garam and a real model.** Enrollment, a
  message and its reply, and restarting a generation without losing
  memory are owed by #1180's next part.
- **Renewing the agent's certificate inside a running adapter.** A
  renewal recreates the adapter; nothing does that yet.
- **A daemon stop or restart.** Measuring one needs a daemon of its
  own, which the shared validation daemon is not.
- **An explicit storage handover.** A refused volume or outbox waits
  for a person, and no command takes that decision.

## Consequences

- The standalone supervisor serves ADR-0084's two routes, so the
  adapter's fenced mode runs on a standalone host with no code of its
  own.
- `internal/workload` is the boundary that owns this. It reaches the
  engine through `Engine` and garam through `Activator`, and its
  Docker implementation is `internal/workload/docker`.
- `make workload-integration` runs the arrangement against a real
  daemon and the pinned images. No workflow runs it, because the
  images come from a registry CI holds no credential for.

## Rejected alternatives

- **Plain HTTP on the socket, authenticated by the placement token
  alone.** The adapter would need a second client, and the leaf garam
  checks would be a file the supervisor read rather than the one the
  adapter presented.
- **A published port, or the gateway on a routable address.** The
  first does not reach a loopback listener, and the second needs a
  gateway certificate any tool could read.
- **No workspace container.** File and exec tools are part of a real
  task.
- **`docker stop` with any timeout.** It kills what the drain exists
  to wait for.
- **Adopting by label, or treating a volume's name as its identity.**
  Anyone with the socket writes a label, and a mount recreates a name.
- **Bind-mounting the adapter's credential.** Under a rootless daemon
  it shows up as owned by container root, so the adapter's key check
  refuses it; `nsenter` to repair that is rejected, and so is running
  the adapter as root.
