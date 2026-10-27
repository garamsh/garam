# ADR 0126: Agents reach models through an AI gateway on OpenRouter, routed by Jev

> Status: proposed
> Date: 2026-10-27

Drafted for #1658, which reserves this number. The direction is the
owner's of 2026-10-27, recorded on #1655, points 8 and 9. garam is
read at `5b596c07962d557b9cced6b867c279fd9371833b`, and Sherlock at
`90e403add20b670fae1794f2e3e7b02961581710`.

## Context

- **No model call passes through garam.** An agent's runtime holds its
  own provider key and calls the provider directly. Sherlock picks the
  vendor by `base-url`, speaks `openai` or `anthropic`
  (`internal/config/config.go:139-149`), and reads its key from the
  environment variable `api-key-env` names (`:152-153`). The agent
  running today calls `MiniMax-M2` at `https://api.minimax.io/v1`
  (#1645).
- **The platform has to own model settings.** An organization chooses
  which models its agents may use, whose key pays, and what it may
  spend. Organizations, policies and audit are the platform's, and a
  setting spread across runtimes can be neither enforced nor
  measured.
- **Routing is where the cost is.** Agents make many calls of uneven
  difficulty. OpenRouter serves `typesafe/jev-router`, which uses
  TypeSafe's decision model, Jev, to pick a model and a reasoning
  effort per request, from a pool a plugin can narrow
  (<https://openrouter.ai/docs/guides/routing/routers/jev-router>).
  The session read it only through search summaries. Its cost and
  latency figures are the vendor's, and no independent benchmark was
  found. It entered limited early access on 2026-09-15.
- **An agent turn constrains routing.** A turn makes several calls,
  and switching models inside one discards the prompt cache. Models
  differ in whether tool calling works.
- **Secrets.** ADR-0036: garam stores a secret's reference, never its
  value, in "an external store the organization operates", and no
  surface serves a value. ADR-0104 lets keys sit in an OpenBao Transit
  provider.

## Decision

**An `ai-gateway` service is the only path from an agent to a model.
It speaks the OpenAI Chat Completions protocol at `ai.garam.sh`,
calls OpenRouter with the platform's key or the organization's own,
and routes by `typesafe/jev-router` within the pool the organization
allows.**

1. **The API.** `https://ai.garam.sh/v1/chat/completions`, OpenAI
   compatible, streaming included. A runtime that speaks `openai`
   points its base URL at it. The model name `auto` asks for routing,
   and any other name asks for that model, if the pool allows it.
2. **Authentication.** An agent presents a short-lived token that
   `platform/` mints for it with the gateway as its audience, as
   ADR-0100 mints tokens today. No provider key reaches a runtime, an
   agent, or any response.
3. **Keys.**
   - **The platform's key** is the deployment's. The gateway reads it
     from the deployment's secret store.
   - **An organization's key (BYOK)** is held in a store the platform
     operates, encrypted under OpenBao Transit. The gateway's database
     holds only its reference, and only the gateway's process reads
     the value. No surface serves it back: the console shows that a
     key is set, its last four characters, and when it was set.
   - This supersedes ADR-0036 in part. A provider key may sit in a
     store the platform operates, not only in one the organization
     operates. The rest of ADR-0036 stands, including that no surface
     serves a secret's value.
4. **Routing at launch.**
   - `auto` resolves through `typesafe/jev-router`, with the pool
     narrowed to the organization's allowed models that are also on
     the gateway's list of models whose tool calling works.
   - **Sticky per turn.** The first call of a turn chooses the model,
     and later calls carrying the same turn identifier reuse it. A
     call that carries none is routed by itself.
   - **Fallback.** A router error, or no answer within a deadline the
     gateway sets, sends the call to the organization's default
     model.
   - **Opt-out.** An organization can turn Jev off. `auto` then means
     its default model.
   - **Later.** When direct providers or custom endpoints arrive, the
     gateway asks `typesafe/jev-1.14` for a tier and maps the tier to
     a model in the organization's own connections. Until then nothing
     is built for it.
5. **Records.** Every call records the organization, the agent's GRN,
   the turn identifier, the model asked for, the model chosen, tokens
   in and out, cost, latency, and whether a fallback was taken. The
   records live in the gateway's own database, and the console reads
   them through the gateway's management API.
6. **Who pays.** Calls on the platform's key are metered and billed
   to the organization. Calls on an organization's key bill nothing
   for the model.
7. **The agent running today** keeps calling MiniMax directly until
   the gateway serves it. It then moves, with no change to Sherlock's
   provider code, once Sherlock can refresh a short-lived token in
   place of a fixed key.

Nothing of this is built. `ai-gateway.md` describes the service, and
implementation is filed once this ADR is accepted.

## Consequences

- Model choice, keys, budgets and usage are one organization setting,
  enforced in one place, and routing's saving is measured against a
  fixed-model baseline from the gateway's own records.
- Every model call takes one more hop, and the gateway's availability
  bounds every agent's. It is deployed apart from the platform's
  management endpoints (ADR-0125).
- Prompts reach OpenRouter, and through Jev, TypeSafe. An organization
  that cannot accept that turns Jev off, or waits for direct
  providers.
- The platform now holds organizations' provider keys, so their
  custody is the platform's: a Transit outage stops BYOK calls, and a
  leak of the gateway's process is a leak of those keys.
- Sherlock must refresh a short-lived token in place of a fixed key.
  That is filed in `garamsh/sherlock`.
- Ruled out: re-implementing OpenRouter's provider routing, and
  runtimes holding provider keys.

## Signs the decision was wrong

- Cost per completed task under `auto` is not lower than under the
  fixed-model baseline, measured over the same kinds of work.
- The fallback takes a large share of calls, or Jev's latency shows
  in turn times.
- Tool-call failures or retries rise after routing is turned on, so
  the pool cannot be kept to models that work.
- Organizations that need direct providers or custom endpoints
  outnumber those that accept OpenRouter, so the launch scope was the
  wrong first step.
