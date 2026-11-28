# ADR 0190: ai-gateway is an interface deployable at garam or in the organization's own environment, and a session has one brain

> Status: accepted; superseded in part by
> [ADR-0195](./0195-a-places-model-gateway-issues-its-own-token-and-adr-0190s-exchange-stays-the-form-at-ai-garam-sh.md),
> on the exchanged token at a gateway outside `ai.garam.sh` (decision 2)
> Date: 2026-11-25

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2027, for which the Garam PM reserved this number. The
owner's decision is
<https://github.com/garamsh/discussions/discussions/6#discussioncomment-19791501>.
garam is read at `dc030cd1c48b8324b207a65a2606448ecb9203ea`, and
garam-bot at `18f95d28443ccdb47b1202ce8cf5bd16dc0db2a0`. A claim below
that names no revision holds for those.

## Context

- **ADR-0126** made an `ai-gateway` service at `ai.garam.sh` the only
  path from an agent to a model. It calls OpenRouter with the
  platform's key or the organization's, routes by Jev, records every
  call and bills the platform's key. None of it is built.
- **ADR-0180** (proposed) left open how a workspace reaches models. It
  named two shapes: a garam model gateway issuing short-lived tokens,
  or a key proxy in garam-bot that holds the key and forwards calls
  (its decision 3).
- **garam-bot's design (garam-bot#1208)** puts model calls on
  interface 5, the model gateway, which is not written yet.
  - A bot's process keeps a token file current for it, and no
    long-lived secret enters a workspace (interface 2's CRED-1).
  - Calls are billed by API through the gateway only, with no
    subscription login, and an organization may bring its own
    provider key, which the gateway holds (`bot-system.md` §Workspaces
    and agents).
  - G3 builds interface 5 and a gateway at the place, and names this
    record as what it needs.
  - A session runs one harness (garam-bot ADR 0035).
- **The owner's decision (comment 19791501)** makes ai-gateway an
  interface:
  - deployable at `ai.garam.sh` or inside the customer's environment,
    so a strict or self-hosted organization keeps call content in its
    environment;
  - a model call goes through it with a short-lived, audience-bound
    token;
  - a session has one brain, chosen when the work starts, and every
    other capability or agent is an MCP tool;
  - BYOK is supported.

## Decision

1. **ai-gateway is an interface, and garam's service is one
   implementation of it.** The interface is garam-bot's interface 5,
   whose specification garam-bot writes with garam (G3). An
   implementation is deployed:
   - at `ai.garam.sh`, as garam's;
   - inside the organization's environment: in its cluster, or built
     into garam-bot at the place.

   A call's content goes no further than the gateway it reaches and
   that gateway's provider, so an organization that must keep content
   in its environment deploys the gateway there.
2. **A model call carries a short-lived token bound to the gateway's
   audience.**
   - A bot garam declared obtains it by exchanging its bot token
     (ADR-0189 decision 2) for a JWT whose `aud` is the gateway's. A
     gateway anywhere verifies it offline against garam's JWKS.
   - The token lives no longer than the bot's token, and the bot's
     process rewrites the token file before it expires.
   - No provider key reaches a workspace, a harness or a response.
3. **A session has one brain**, the agent harness chosen when the work
   starts. Every other capability, another agent among them, reaches
   it as an MCP tool (garam-bot ADR 0035) and is no second model loop
   inside the session.
4. **BYOK at every deployment.** An organization's provider key is held
   by the gateway it uses. At `ai.garam.sh` it is held as ADR-0126
   decision 3 holds it. In the organization's environment, its own
   secret store holds it and garam never sees it. Calls are billed by
   API through a gateway, never by a subscription login.
5. **This supersedes ADR-0126 in part:**
   - decision 1's single endpoint as the only path, now an interface
     with deployments;
   - decision 2's token, now the audience-bound exchange above;
   - decision 7's migration of the agent running then, whose in-house
     loop garam-bot ADR 0040 removes.

   ADR-0126's keys, routing, records and billing (decisions 3 to 6)
   stand for garam's deployment at `ai.garam.sh`. A deployment in an
   organization's environment records and routes as interface 5 has
   it.
6. **This decides ADR-0180's decision 3** for the gateway: a workspace
   reaches models through ai-gateway with a token bound to it. The key
   proxy's job, holding the key away from the workspace, is the
   gateway's when it runs inside garam-bot at the place.

## Consequences

- garam's `ai-gateway` is built against interface 5 once garam-bot
  writes it. ADR-0126's implementation issue (#1655) builds to that
  specification.
- garam's issuer exchanges a bot's token for a gateway-bound JWT. A
  gateway outside garam needs only garam's JWKS, and no call to garam
  per model call.
- A gateway in an organization's environment reports no records to
  garam unless interface 5 carries them. Spend at that deployment is
  the organization's to see.
- ADR-0180 stays proposed for its registry. Its decision 3 is decided
  here, and its status says so.

## Signs the decision was wrong

- Organizations run the gateway in their own environment and still
  want garam's records and budgets, so interface 5 grows a reporting
  path that makes the deployment split moot.
- One brain per session forces harnesses to rebuild a second model
  loop as a tool so often that the rule is read around.
- A token bound to one gateway's audience is refused by a gateway
  deployed under another name often enough that the audience needs
  widening.

## Rejected alternatives

- **The key proxy in garam-bot as the only shape (ADR-0180's second
  candidate).** It would leave `ai.garam.sh` with no role for an
  organization without an environment of its own. As one deployment of
  the interface, it keeps its point.
- **Keeping ADR-0126's single endpoint.** A strict or self-hosted
  organization's call content would have to leave its environment.
