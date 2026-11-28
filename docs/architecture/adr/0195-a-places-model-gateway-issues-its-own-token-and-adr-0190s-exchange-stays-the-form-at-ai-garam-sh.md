# ADR 0195: A place's model gateway issues its own token, and ADR-0190's exchange stays the form at ai.garam.sh

> Status: accepted
> Date: 2026-11-29

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for garam-bot#1211, item G3 of garam-bot's
`docs/architecture/bot-system.md` §Goals ④ and ⑤ at
`012314edfd20a78129b260bd737cbaf4a9e41012`. The garam-bot PM's approval
of G3's design on garam-bot#1211 names this number. garam is read at
`ca336eedb423ebb6a177c8701ba33fc2d6412729`, and garam-bot's interface 5
at garam-bot#1304's head,
`2ccef492134bfed3459586dddff3f95aa4ee4c12`. A claim below that names no
revision holds for those.

## Context

- **ADR-0190 decision 2** has a model call carry a short-lived token
  bound to the gateway's audience. A bot garam declared obtains it by
  exchanging its bot token at garam, and a gateway anywhere verifies
  it against garam's JWKS.
- **That exchange cannot serve a gateway at the place.**
  - A bot declared at its place has no garam token (garam-bot
    `docs/architecture/bot-system.md` §Authentication).
  - Every token garam issues to a bot's process is bound to that
    process's key ([ADR-0191](./0191-every-token-garam-issues-to-a-bots-process-is-bound-to-that-processs-key-by-dpop.md)).
    The harness in a workspace cannot sign a proof with it, so no
    such token may enter a workspace.
  - A gateway at the place holding the organization's own key needs
    no garam for a model call.
- **garam-bot's interface 5** (`api/model-gateway/v1alpha1/README.md`,
  garam-bot ADR 0044) states the model token abstractly (TOKEN-1):
  - it lives at most an hour;
  - it is of the gateway's own audience;
  - it is checked by that gateway alone.

  A place's gateway issues it through interface 5's issue surface, to
  the place's issuer, which authenticates with a credential of the
  place.
- **v1 serves the organization's own key alone** (cupa on
  garam-bot#1211, 2026-11-28, comment 7234259311).

## Decision

1. **A model gateway at the place issues its own model token**, to its
   place's issuer, through interface 5's issue surface. No garam token
   is exchanged for it, and garam neither issues nor verifies it.
2. **At `ai.garam.sh` the model token stays ADR-0190's exchange.** A bot
   garam declared exchanges its bot token for a JWT whose `aud` is
   garam's gateway's, which interface 5's TOKEN-1 admits.
3. **What a token is lives in interface 5**, not in garam. A gateway
   anywhere answers a token's life, audience and check as TOKEN-1
   states them, and garam's `ai-gateway` builds to that statement.
4. **This supersedes ADR-0190 decision 2 in part:** its exchange as how
   every gateway's token is obtained, and verification against garam's
   JWKS by "a gateway anywhere". Decision 2 stands for `ai.garam.sh`,
   and its third point, that no provider key reaches a workspace, a
   harness or a response, stands at every deployment.

## Consequences

- garam's issuer exchanges a bot's token for a model token only for
  `ai.garam.sh`. A gateway at the place needs no garam JWKS and no call
  to garam.
- A gateway at the place reports nothing to garam. Whether interface 5
  carries usage to garam is an open question there, as ADR-0190
  §Consequences left it.
- A bot declared at its place reaches a model at its place with no
  garam declaration.

## Signs the decision was wrong

- Organizations running a gateway at the place want garam to revoke
  or audit its tokens, which a token garam never sees cannot give.
- Gateways at the place and at `ai.garam.sh` drift in what a token
  means often enough that the abstract statement is read around.

## Rejected alternatives

- **ADR-0190's exchange at every place.** A bot declared at its place
  has no garam token, and every token garam issues to a bot's process
  is bound to a key no harness can sign with (ADR-0191), so a place's
  gateway would need garam for a call that does not.
- **Removing the exchange at `ai.garam.sh` too.** garam's gateway would
  need an issuer at every place to call it, where today the bot's
  process already holds a garam token to exchange.
