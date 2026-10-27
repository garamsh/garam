# AI gateway

How agents reach AI models: the `ai-gateway` service, the keys it
holds, how it routes, and what it records (ADR-0126).

**None of this is built.** No `ai-gateway/` folder exists, and an agent
today holds its own provider key and calls the provider directly
(#1645 records `MiniMax-M2` at `https://api.minimax.io/v1`).
Implementation is filed under #1655 now that ADR-0126 is accepted.
This document moves to `ai-gateway/docs/` with the service
([ADR-0125](./adr/0125-the-repository-is-a-monorepo-of-services-laid-out-by-responsibility.md)).

## Contents
- Current decisions
- Rationale
- What other repositories owe
- Open questions

## Current decisions

- **The only path from an agent to a model.** The gateway serves
  `https://ai.garam.sh/v1/chat/completions`, OpenAI compatible and
  streaming included. A runtime that speaks that protocol points its
  base URL there.
- **What an agent asks for.** The model name `auto` asks the gateway to
  route. Any other name asks for that model, which is refused unless
  the organization's pool allows it.
- **Authentication.** An agent presents a short-lived token that
  `platform/` mints with the gateway as its audience, as it mints
  agent tokens under ADR-0100. No provider key reaches a runtime, an
  agent or a response.
- **Keys.**
  - The platform's OpenRouter key is the deployment's, read from the
    deployment's secret store.
  - An organization's own OpenRouter key (BYOK) is held encrypted
    under OpenBao Transit in a store the platform operates. The
    gateway's database holds its reference, and only the gateway's
    process reads the value. The console shows that a key is set, its
    last four characters, and when it was set, and nothing else.
- **Routing.**
  - `auto` resolves through OpenRouter's `typesafe/jev-router`, with
    its pool narrowed to the organization's allowed models that are
    also on the gateway's list of models whose tool calling works.
  - The first call of a turn chooses the model, and later calls
    carrying the same turn identifier reuse it. A call carrying none
    is routed by itself.
  - A router error, or no answer within the gateway's deadline, sends
    the call to the organization's default model.
  - An organization can turn Jev off, and `auto` then means its
    default model.
- **Records.** Each call records the organization, the agent's GRN, the
  turn identifier, the model asked for and the model chosen, tokens in
  and out, cost, latency, and whether a fallback was taken. The
  gateway's own database holds them, and the console reads them
  through the gateway's management API.
- **Billing.** Calls on the platform's key are metered and billed to
  the organization. Calls on an organization's key bill nothing for
  the model.
- **Deployment.** The gateway is a runtime endpoint, deployed apart
  from the management endpoints (`structure.md` §Services).

## Rationale

- One place for model settings, keys and spend, because organizations,
  policies and audit are the platform's, and a setting spread across
  runtimes can be neither enforced nor measured (ADR-0126).
- OpenRouter at launch rather than our own provider routing: routing
  across providers is commodity, and keeping it up costs more than it
  differentiates (#1655).
- `typesafe/jev-router` for `auto`, chosen per turn and not per call,
  because switching models inside a turn discards the prompt cache
  (ADR-0126).
- A provider key may sit in a store the platform operates, which
  ADR-0126 decides in place of ADR-0036's "a store the organization
  operates". The rest of ADR-0036 stands: no surface serves a
  secret's value.

## What other repositories owe

- `garamsh/sherlock` reads its model key once, from an environment
  variable, when it builds the client
  (`cmd/sherlock/model_provider.go:24` and `:41` at
  `90e403add20b670fae1794f2e3e7b02961581710`). A token that expires
  needs re-reading: garamsh/sherlock#1161.

## Open questions

- **Where the turn identifier comes from.** The runtime has to send
  one for the choice to stay fixed across a turn. Its header name and
  who mints it are settled with garamsh/sherlock#1161 or the first
  implementation pull request.
- **Direct providers and custom endpoints.** ADR-0126 names the shape:
  the gateway asks `typesafe/jev-1.14` for a tier and maps it to a
  model in the organization's own connections. Nothing is decided
  beyond that, and nothing binds until a later ADR takes it up.
