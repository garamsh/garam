# AI gateway

How a bot reaches AI models: ai-gateway, an interface deployable at
garam or in an organization's own environment, and garam's
implementation of it, with the keys it holds, how it routes and what
it records
([ADR-0190](./adr/0190-ai-gateway-is-an-interface-deployable-at-garam-or-in-the-organizations-own-environment-and-a-session-has-one-brain.md),
ADR-0126).

**None of this is built.** No `ai-gateway/` folder exists, and an agent
today holds its own provider key and calls the provider directly
(#1645 records `MiniMax-M2` at `https://api.minimax.io/v1`).
Implementation is filed under #1655 now that ADR-0126 is accepted.
This document moves to `ai-gateway/docs/` with the service
([ADR-0125](./adr/0125-the-repository-is-a-monorepo-of-services-laid-out-by-responsibility.md)).

ADR-0190 supersedes ADR-0126 in part, on the single endpoint, the
token and the migration of the agent running then. It also decides
[ADR-0180](./adr/0180-garam-adds-a-workspace-registry-and-how-a-workspace-reaches-models-is-open.md)'s
open question for the gateway. ADR-0126's keys, routing, records and
billing govern garam's deployment at `ai.garam.sh`.

## Contents
- Current decisions
- Rationale
- What other repositories owe
- Open questions

## Current decisions

- **An interface with deployments** (ADR-0190). ai-gateway is
  garam-bot's interface 5, whose specification garam-bot writes with
  garam and which is not written yet. It is deployed:
  - at `ai.garam.sh`, garam's, serving
    `https://ai.garam.sh/v1/chat/completions`, OpenAI compatible and
    streaming included;
  - in an organization's environment: its cluster, or built into
    garam-bot at the place, where call content stays.
- **A session has one brain**, the agent harness chosen when the work
  starts. Every other capability, another agent among them, reaches it
  as an MCP tool (ADR-0190 decision 3).
- **What an agent asks for.** The model name `auto` asks the gateway to
  route. Any other name asks for that model, which is refused unless
  the organization's pool allows it.
- **Authentication.** A bot garam declared exchanges its bot token
  ([ADR-0189](./adr/0189-a-bot-garam-declares-is-an-iam-principal-whose-connector-exchanges-for-its-token-and-one-cedar-policy-decides-what-it-may-do.md)
  decision 2) for a JWT whose `aud` is the gateway's, which any
  deployment verifies offline against garam's JWKS. It lives no
  longer than the bot's token, and the bot's process rewrites its
  token file. No provider key reaches a workspace, a harness or a
  response.
- **Keys at `ai.garam.sh`** (ADR-0126). In an organization's
  environment, its own secret store holds its provider key, and garam
  never sees it (ADR-0190 decision 4).
  - The platform's OpenRouter key is the deployment's, read from the
    deployment's secret store.
  - An organization's own OpenRouter key (BYOK) is held encrypted
    under OpenBao Transit in a store the platform operates. The
    gateway's database holds its reference, and only the gateway's
    process reads the value. The console shows that a key is set, its
    last four characters, and when it was set, and nothing else.
- **Routing at `ai.garam.sh`.**
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
- **Records at `ai.garam.sh`.** Each call records the organization,
  the bot's GRN, the turn identifier, the model asked for and the
  model chosen, tokens in and out, cost, latency, and whether a
  fallback was taken. The
  gateway's own database holds them, and the console reads them
  through the gateway's management API.
- **Billing.** Calls are billed by API through a gateway, never by a
  subscription login. Calls on the platform's key are metered and
  billed to the organization. Calls on an organization's key bill
  nothing for the model.
- **Deployment.** The gateway is a runtime endpoint, deployed apart
  from the management endpoints (`structure.md` §Services).

## Rationale

- An interface rather than one endpoint, because a strict or
  self-hosted organization keeps call content in its environment
  (ADR-0190).
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

- garam-bot writes interface 5 with garam, and builds a gateway at the
  place (G3 in garam-bot `docs/architecture/bot-system.md` at
  `18f95d28443ccdb47b1202ce8cf5bd16dc0db2a0`).

## Open questions

- **What a deployment in an organization's environment reports to
  garam.** Its records and spend stay there unless interface 5 carries
  them.

- **Where the turn identifier comes from.** The runtime has to send
  one for the choice to stay fixed across a turn. Its header name and
  who mints it are settled with garamsh/sherlock#1161 or the first
  implementation pull request.
- **Direct providers and custom endpoints.** ADR-0126 names the shape:
  the gateway asks `typesafe/jev-1.14` for a tier and maps it to a
  model in the organization's own connections. Nothing is decided
  beyond that, and nothing binds until a later ADR takes it up.
