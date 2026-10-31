# ADR 0129: cowork acts in each organization as a principal the platform issues with it, proving one deployment key

> Status: accepted; §7's last sentence, on an open WebSocket, is
> superseded by
> [ADR-0139](./0139-an-open-cowork-websocket-is-asked-again-within-a-stated-bound.md)
> Date: 2026-10-28

Drafted for #1664; #1655 reserves this number. The direction is the
owner's, recorded on #1655 in comment 6456001127 (point 4) of
2026-10-27, as revised in comment 6464161444 of 2026-10-28. garam is
read at
bfa391400fd84d43cb52d961154325da7f2fa022; a claim below that names no
revision holds for it. No implementation ships in this change.

## Context

- **cowork has to reach agents.**
  [ADR-0127](./0127-cowork-is-a-service-where-people-and-agents-co-edit-message-and-work-through-tools-an-ai-can-use.md)
  §Decision point 6 posts a message that mentions an agent, or a
  direct message with one, to that agent's queue through
  `AgentDelivery`, "under a service identity that `platform/` issues
  to cowork", a new kind of machine principal built in #1664. #1666's
  task-assignment notices take the same path. Until it exists, cowork
  keeps a message's agent mentions and answers them as
  `undeliveredMentions` ([cowork.md](../../../cowork/docs/cowork.md)
  §What it asks of the platform).
- **Every other act in cowork is the caller's.** cowork asks the
  platform `checkPermissions` with the caller's own credential before
  every read and write (ADR-0127 §5,
  [ADR-0128](./0128-a-cowork-channels-participants-narrow-what-the-platform-allows-and-grant-nothing.md)).
  An agent answers through cowork's MCP tools as itself, with the
  browser token
  [ADR-0123](./0123-an-agent-calls-the-browser-api-and-the-cli-as-itself-authorized-by-its-own-policies.md)
  decides. Delivering a mention is the one act cowork makes on its own
  account.
- **The queue is the machine listener's.** `postMessage` stores a
  message under the authenticated caller as its sender, and the
  signature on every hand-out names that sender as the one garam
  vouches for
  ([ADR-0101](./0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md),
  [messaging.md](../messaging.md)). The listener asks the engine
  `message:send` about every recipient of the caller's organization,
  on an Allow the caller's own GRN grants it there, and refuses a
  recipient of another organization before it asks
  ([ADR-0024](./0024-policy-decides-what-an-agent-sends.md)). A
  caller that is not an agent passes no execution fence
  (`platform/internal/machineapi/execution.go` `fenceOf`).
- **Machine credentials are garam-issued and short-lived.** An
  operator proves a key by `private_key_jwt` at
  `exchangeClientAssertion` and holds one-hour opaque tokens read on
  every request beside the key and record they rest on
  ([ADR-0100](./0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md),
  [ADR-0102](./0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)).
  ADR-0123 named this ADR as a later consumer of its browser-audience
  token.
- **What the owner decided** (#1655):
  - cowork is a first-party service every organization has. No
    organization installs it or switches it off. Stopping one
    organization's cowork is an operational act, revoking that
    organization's cowork principal.
  - One principal per organization, issued by the platform when the
    organization is created; a migration issues one to every existing
    organization.
  - cowork proves one deployment key by `private_key_jwt` and receives
    short-lived tokens scoped to one organization.
  - Policy and audit stay per organization under ADR-0024. One global
    principal would cross the organization boundary for every
    organization but one, and audit would no longer separate them.

## Decision

**Each organization has one cowork principal, a record the platform
writes with the organization. cowork exchanges an assertion signed by
its one deployment key, naming that principal, for a one-hour token
the machine listener accepts. With it cowork posts to the queues of
that organization's agents, decided like any machine sender's, inside
a floor no policy lifts. The deployment, not the organization, holds
the key and revokes the principal.**

1. **The principal.**
   - Its GRN is `grn:<org>:default:service:cowork`: owned by the
     organization (ADR-0001), of a new service type `service`, its
     identifier the service's name. cowork derives it from the
     organization and needs no lookup.
   - The platform writes its record in the transaction that creates
     the organization. A migration writes one for every organization
     that exists. No route creates one, and none issues a secret per
     organization: the record is what admits the deployment key's
     assertions for that organization.
   - Deleting the organization deletes the record by the cascade that
     takes its agents and operators, and every token resting on it.
2. **The deployment key.**
   - One RSA key pair, as ADR-0102's profile takes it. cowork
     generates it where it is deployed and keeps the private key in an
     owner-only file it reads per exchange. No environment variable,
     image layer or command argument carries it, and it never passes
     through garam.
   - The machine listener is configured with the public half: a file
     naming each first-party service's public JWKs, read per exchange,
     so a change takes no restart. At most two keys per service are
     live, for a rotation, which follows ADR-0102's: install the
     successor, prove an exchange, then remove the old one. A
     compromise removes the key at once.
   - The deployment's GitOps writes both files. No organization, role
     or policy reaches the key, and no route on either listener
     issues, lists or withdraws it.
3. **The exchange and the token.**
   - cowork calls `exchangeClientAssertion` under `machine-token.v2`.
     `client_id`, `iss` and `sub` are the organization's principal
     GRN; every other member is ADR-0102's, the one-use `jti` included.
     The key is looked up by `kid` among the service's configured keys
     rather than in an operator's registration.
   - The token lives one hour, as an operator's does, and has a prefix
     of its own. Its row names the principal, the record's
     incarnation and the key's `kid`. Every request reads it joined to
     that record and checks the key is still configured, so revoking
     the principal, removing the key or deleting the organization
     refuses it from the next request. Re-issuing a revoked principal
     revives no earlier token.
   - **Its audience is the machine listener alone.** ADR-0123's browser
     audience is not issued to it. Every browser read cowork makes is
     on a caller's behalf and is decided as that caller (ADR-0127 §5);
     a standing principal there would be a deputy able to read what no
     caller asked for. A later ADR that needs one decides it under
     ADR-0123's mechanism.
4. **Authorization is a machine sender's** (ADR-0024).
   - `postMessage` asks the engine `message:send` with the principal's
     GRN as subject and the recipient as object, on the Allow its own
     GRN grants it in its organization.
   - Policies attached to the principal apply. A Deny narrows what it
     may deliver, to one agent or to all of them: that is the
     organization's policy over delivery, not a switch over cowork.
     An Allow widens nothing past the floor.
   - **Under ADR-0130** the principal is a `Service` entity of its
     organization. Its sending is the system policy ADR-0024's
     baseline becomes, an organization narrows it with a `forbid`, and
     the floor below is a system `forbid`. Nothing here waits on
     ADR-0130.
5. **The floor.** Refused before the engine is asked, whatever a
   policy says:
   - **Its own organization only.** A recipient of another
     organization is refused, and no consent relation (ADR-0035)
     carries the principal across: a workspace is one organization's
     (ADR-0127 §4).
   - **Agents only.** A recipient that is not an agent of its
     organization is refused.
   - **Three operations.** Its token reaches `postMessage`,
     `getMessageStatus` for a message it sent, and `getIdentity`, and
     nothing else on the machine listener: no claim of any queue, no
     durable submission, no mint, enrollment, lifecycle or operator
     route. It reaches nothing on the browser API.
6. **Every record names who did what.**
   - The message's sender, its send event (ADR-0106) and the signature
     an agent verifies name the principal, because delivering is
     cowork's act and garam vouches only for the caller it
     authenticated.
   - The person or agent who wrote the cowork message is named in the
     body cowork writes, beside the message's address in cowork. garam
     neither reads nor vouches for it. An agent that needs it confirmed
     reads the message through cowork's tools as itself, where cowork
     answers what its record holds and the agent may read.
   - The principal's token issuances and refusals are credential
     events with the principal as subject (ADR-0114), and its
     exchanges are authentication evidence (ADR-0109), each under its
     organization.
7. **Stopping one organization's cowork is the deployment's act.**
   - A server command revokes an organization's principal, and
     another re-issues it. No browser operation does either, and no
     role or policy reaches them.
   - While it is revoked, the platform refuses every `cowork:` action
     on that organization before the engine is asked. cowork asks the
     platform before every read and write (ADR-0127 §5), so the
     organization's whole workspace stops, not only delivery. An open
     WebSocket stops at its next reconnect, as a policy change does.
8. **What #1664 and #1666 build on.**
   - #1664 builds the principal and its migration, the key
     configuration, the exchange's service client, the token read, the
     floor and the revocation commands, and cowork's `AgentDelivery`
     adapter, which posts each mention under the organization's
     principal and resolves `undeliveredMentions`.
   - #1666 posts a task-assignment notice to the assignee agent's queue
     through the same port and principal, and builds nothing else here.

## Consequences

- cowork delivers to agents with no install step and no per-organization
  setup, and each organization's deliveries are attributable to that
  organization's principal in its own records.
- **The deployment key reaches every organization.** Whoever holds it
  can exchange for any organization's principal. That is the cost of
  one key, which the owner chose; the floor bounds what any such token
  can do to posting into that organization's agents' queues.
- An agent's message from cowork names cowork as its sender. The
  author inside it is cowork's claim, which the agent checks against
  cowork when it matters.
- An organization narrows cowork's delivery by policy and cannot turn
  cowork off. The deployment turns it off for one organization, and
  then nobody in that organization can use cowork.
- Every request from cowork to the machine listener costs the
  indexed read ADR-0100 measured, and the exchange is bounded per
  process as ADR-0102's is.
- `grn-model.md` gains the `service` type. A later first-party service
  takes a principal of the same form, under an ADR of its own.
- The responsibility documents this changes are paired in this pull
  request: [cowork.md](../../../cowork/docs/cowork.md),
  [machine-identity.md](../machine-identity.md),
  [organizations.md](../organizations.md),
  [authz.md](../authz.md), [messaging.md](../messaging.md),
  [audit.md](../audit.md), [api-surface.md](../api-surface.md) and
  [grn-model.md](../grn-model.md).

## Signs the decision was wrong

- The deployment key leaks and every organization's principal has to
  be cut off at once, often enough that a key per organization was
  worth its provisioning.
- cowork needs to read or write the platform as itself for a feature,
  and the machine-listener-only audience keeps being worked around.
- Organizations ask to stop cowork themselves, and the deployment's
  revocation is used as a setting.
- Agents act on the author cowork names without checking it, and a
  forged author is found in a delivered message.
- A message from a cowork principal is found in another
  organization's queue.

## Rejected alternatives

- **An installation an admin removes.** cowork is first-party and
  every organization's sidebar links it (#1683), so an install step
  only adds a state, and removing it would be an organization switching
  cowork off, which the owner ruled out.
- **One global cowork principal.** ADR-0024 refuses a recipient of
  another organization before the engine is asked, and a machine
  identity's GRN grants it only its own organization; a global
  principal would cross that line for every organization but one, and
  audit would no longer separate organizations.
- **A key per organization.** It bounds a leak to one organization,
  and costs a key to provision, store and rotate for every
  organization, which the owner declined for one deployment key.
- **garam issuing the deployment key as a PEM**, as ADR-0102 issues an
  operator's. It puts the private key through garam and needs a route
  or command that issues it; generating it where cowork runs and
  configuring the public half needs neither, and ADR-0102 itself
  recorded local generation as the safer custody.
- **cowork posting under the author's GRN.** garam would vouch that a
  person or an agent sent what only cowork sent, and one deployment
  key could speak as any member of any organization.
- **The browser audience as well.** See §Decision point 3.
- **Revoking the principal stopping delivery alone.** People would go
  on using a workspace the deployment meant to stop, against the
  owner's reading that revoking the principal stops that
  organization's cowork.
