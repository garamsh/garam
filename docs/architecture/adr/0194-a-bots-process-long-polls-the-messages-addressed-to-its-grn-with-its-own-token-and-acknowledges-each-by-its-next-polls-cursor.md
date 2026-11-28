# ADR 0194: A bot's process long-polls the messages addressed to its GRN with its own token, and acknowledges each by its next poll's cursor

> Status: accepted
> Date: 2026-11-28

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2075, item G4 of garam-bot's
`docs/architecture/bot-system.md` §Goals at
`18f95d28443ccdb47b1202ce8cf5bd16dc0db2a0`. The Garam PM's brief
directs the next free number.

## Context

garam-bot's design leaves open whether a bot's process long-polls
garam for its messages itself or its connector carries every bot's
(`bot-system.md` §Open questions, "Messages to a bot"), and #2075 takes
the bot's own long poll, which the design's goal list names.

What a message to a bot is in garam is already decided. The queue
([ADR-0013](./0013-agent-messaging.md)) holds every message by its
recipient's GRN and checks no recipient against a record, and a bot
garam declared is `grn:<org>/iam/bot/<declaration id>`
([ADR-0189](./0189-a-bot-garam-declares-is-an-iam-principal-whose-connector-exchanges-for-its-token-and-one-cedar-policy-decides-what-it-may-do.md)).
`postMessage` admits a sender's post to it without asking the policy
service, since the recipient names no agent garam holds
([ADR-0168](./0168-the-machine-listeners-send-is-the-policy-services-decision-consent-included.md)),
and stores it. Nothing reads it: `claimMessage` serves an agent's GRN
to a machine token, and a bot holds neither. No console route posts a
message, and cowork's delivery is unbuilt.

An agent's adapter holds a claim under a lease, renews it while a turn
runs and completes it (ADR-0018). A bot's process is one reader of its
own mailbox over a connection it opens, and #2075 asks that it resume
after a dropped connection from a cursor.

[ADR-0191](./0191-every-token-garam-issues-to-a-bots-process-is-bound-to-that-processs-key-by-dpop.md)
decision 6 binds the first route that accepts a bot's token, and this
is that route.

Three alternatives were weighed:

- **The connector carries every bot's messages.** It keeps one
  connection per place, but the connector would read messages addressed
  to principals it is not, and garam-bot's list names the bot's own
  poll.
- **`claimMessage` takes a bot's token.** Its path names an agent, its
  security is the machine token and the agent's execution fence, and a
  bot would make three calls per message where one poll does.
- **A log per bot at positions, as ADR-0187's declarations.** It is a
  second store for messages the queue already keeps once per key,
  signs, sweeps and answers the state of.

## Decision

1. **A message to a bot is a message of the queue addressed to the
   bot's GRN.** Nothing new stores one, and who may send one is what
   `postMessage` already admits.
2. **The bot's process long-polls `pollBotMailbox`,
   `POST /bots/{bot}/mailbox`, with its own token**, and polls its own
   mailbox alone: the caller is the bot the token names, and another
   bot's mailbox is refused 403.
3. **The cursor is the hand-out's claim, and the next poll naming it
   acknowledges the message.** It is completed and its body dropped, as
   ADR-0034 drops a delivered body. A bot's mailbox has one reader, so
   each poll first hands out again every message the bot was handed
   and did not acknowledge, and a process whose answer was lost with
   its connection is handed that message before any later one. Each
   hand-out spends an attempt under ADR-0018's bound. A cursor whose
   message was handed out again since is refused 409, acknowledging
   and claiming nothing.
4. **The wait is the queue's**, 30 seconds, and an empty one is
   answered 204.
5. **The route holds ADR-0191 decision 6.** The token arrives as
   `Authorization: DPoP`, and `Bearer` is refused. The proof is by the
   key the token is bound to, for the request's method and its URL at
   the listener's external origin, `GARAM_MACHINE_ISSUER`, with `ath`
   the token's hash, and its `jti` is spent once the token resolved. A
   refusal is 401 with a `WWW-Authenticate` challenge.
6. **Every hand-out is signed as ADR-0101 signs a claim**, with the bot
   as audience, and no part of a message's content is logged.

## Consequences

- garam-bot binds to `platform/api/machine.yaml` §pollBotMailbox, the
  contract of record for the route.
- A bot's process makes one call per message, renews nothing and
  completes nothing apart from its next poll.
- Two processes polling for one bot are each handed the other's
  messages. A process restarted under a new key polls on, with a new
  token, from no cursor.
- A connection that drops repeatedly on one message spends its
  attempts, three by default, and the message is then not handed out
  again, as an agent's is not.
- A bot is reached by an agent of its own organization. Across the
  boundary `postMessage` refuses a recipient that is not an agent, so
  no consent reaches a bot, and a bot's own policy decides nothing it
  receives.
- A bot posts nothing yet: no route accepts its token but this one.

## Signs the decision was wrong

- Bots run more than one process each, or are restarted under a new
  key often enough that a message is handed out twice to two holders.
- Messages to bots are seen exhausted by dropped connections rather
  than by failing turns.
- A place runs bots in numbers whose one connection each costs it more
  than the connector carrying them would.
- A bot needs messages from another organization, or a policy deciding
  what it receives, and the queue's admission has to change for it.
