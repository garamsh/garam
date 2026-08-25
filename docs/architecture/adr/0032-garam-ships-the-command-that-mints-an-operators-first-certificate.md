# ADR 0032: garam ships the command that mints an operator's first certificate

> Status: accepted
> Date: 2026-08-18

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0015](./0015-operator-identity.md) left one step out of band: the
operator's own first certificate, "installed out of band, once", the
single manual bootstrap that removes every per-agent one. When it was
written nothing performed that step, and the note was honest because
the surface it bootstraps did not exist. By the time the machine
surface was implemented and verified, the gap had inverted: everything
ADR-0015 describes worked, and no one could reach any of it with what
the repository ships. The measure of the gap was in the test suite —
the e2e tests carry their own helper to do what the product could not
— and in every manual verification of the surface requiring a
throwaway program against `internal/ca` (#254).

ADR-0015 also fixed the property that makes shipping this dangerous:
no issuing credential exists to be stolen. A command that mints any
identity is exactly that credential. The decision ADR-0015 declined —
whether garam ships a binary that can mint identities — is what this
ADR takes.

## Decision

`garam issue-certificate <subject-grn> --out-dir <dir>` performs the
bootstrap. Two constraints preserve what ADR-0015 and
[ADR-0016](./0016-ca-key-custody.md) decided:

- **Operator subjects only.** The subject's GRN service type is
  checked before the command touches the database or the key, and a
  non-operator subject is refused. A general minting command would be
  the issuing credential ADR-0015 refuses to create; an agent's
  certificate keeps its existing path, the machine surface an assigned
  operator calls.
- **No serving process can reach it.** The command is a root-level
  subcommand no serve path references, so a running server cannot mint
  an identity; minting is reachable only by someone who already holds
  the database URL and the key-encryption key file — the custody
  ADR-0016 already grants `serve machine`.

The key-encryption key arrives the way ADR-0015 has key material
arrive: a file its owner alone can read, enforced by the same sealer
`serve machine` starts under. The private key it writes is owner-only
(`0600`) for the same reason.

## Consequences

- The machine surface is reachable by a contributor following the
  repository, which is the precondition for any manual verification of
  ADR-0013/0014/0015 behaviour against a real deployment.
- Minting is an act that requires the same custody as running the
  machine process; it grants no caller anything new over the network.
- The e2e suite exercises the shipped command rather than a private
  helper, so the bootstrap path is tested where it ships.

## Rejected alternatives

- **A written procedure only.** Leaves every contributor to re-derive
  the sealing details against `internal/ca`, which cannot be imported
  from another module — the cost this decision removes, already paid
  twice (the test suite's helper, the throwaway verification program).
- **Minting agent certificates through the same command.** That is the
  issuing credential ADR-0015 refuses to create, arriving by default
  rather than by decision.
- **A `make` target wrapper.** A convenience over the command, not a
  shape of its own; nothing about the decision changes inside one.

## Errata

- 2026-08-26 — §Decision's "No serving process can reach it" states that
  "a running server cannot mint an identity". The premise is true — the
  command is a root-level subcommand no serve path references — but the
  conclusion drawn from it is wider than it supports. `garam serve
  machine` mints agent identities and issues certificates on the
  listener it serves: `POST /agents` answered `201` with a freshly
  minted GRN on the dev cluster, and `internal/machineapi/agent.go`
  reaches `internal/agent/service.go`'s `NewGRN` and `issuer.Issue`.
  What the premise does support, and what the decision rests on, is
  narrower: no serving process mints an operator's **first**
  certificate, which is the bootstrap ADR-0015 leaves out of band and
  the reason this command is a sibling of `serve` rather than a
  subcommand under it. The decision stands on that. The same clause was
  corrected in `docs/architecture/domains.md` (#557),
  `internal/cli/cli.go` and `internal/cli/issue_certificate.go` (#561).
