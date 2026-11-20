# ADR 0177: The earlier policy engine, its serve command, its protobuf module and its tables are removed

> Status: accepted; built in `platform` (#2000); builds
> [ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md)
> item 1 and supersedes
> [ADR-0003](./0003-single-cli-topology.md) in part, on `serve authz`
> and the gRPC contract between the processes
> Date: 2026-11-17

Drafted for #2000, which reserves this number, as round 14 of #1796.
[authz.md](../authz.md) states the rule beside the code it governs.

## Context

ADR-0130 replaced the platform's policy engine with the policy
service, and ADR-0146 decided that the platform decides through the
service alone. Over rounds 1 to 13 of #1796 every caller moved: the
machine listener's send (ADR-0168), delegated operation authority and
the controller proof (ADR-0146 items 14 and 15), the browser listener's
enforcement, `checkPermissions`, the holders list and the lists
(ADR-0171, ADR-0172), the explanation (ADR-0174), the policy writes
(ADR-0169, ADR-0173), the audit record (ADR-0176), the policy search
and the organization deletion's count (#1990) and `iam:permit.write`
(#1995). `garam serve api` and `garam serve machine` started and were
ready without the engine (#1999).

What remained was code and state nothing called: `garam serve authz`,
`platform/internal/authz` with its store, cache and change listener,
the protobuf module `platform/proto/garam/authz` and its generated
code with the `buf` configuration that generated it, the environment
variables `GARAM_AUTHZ_ADDR`, `GARAM_GRPC_ADDR` and
`GARAM_METRICS_ADDR` that only they read, the tables `policies`,
`policy_versions` and `policy_attachments` that nothing wrote since
ADR-0174, and the query `ListProjectsByMember`, whose last caller was
the engine's statement collection. The issue
names `1273818c0b76f433c38faadaac79c8c00e2bf4ee` as the engine's code
to read; `6595be18cdb68b264057e8c94f73ad9d165d0840` (#1981) touched the
package after it.

## Decision

1. **The engine goes.** `platform/internal/authz`, `garam serve
   authz`, `platform/proto/garam/authz` with its generated code,
   `buf.yaml` and `buf.gen.yaml`, and the `wire` types `Effect`,
   `PolicyStatus` and `Statement` that only the engine's contract used
   are deleted. The platform has no protobuf module and no gRPC server
   of its own. The `buf`, `protoc-gen-go` and `protoc-gen-go-grpc` pins
   stay: `lib/policy-sdk-go` generates with them and `policy` lints with
   `buf`.
2. **Its settings go.** `GARAM_AUTHZ_ADDR`, `GARAM_GRPC_ADDR` and
   `GARAM_METRICS_ADDR` are no longer read. A deployment that sets one
   sets nothing the process reads.
3. **Its tables go, and the migration is reversible empty.** Migration
   000081 drops `policy_attachments`, `policy_versions` and
   `policies` and the functions their triggers called, including the
   one that raised `garam_policy_change`. Its `down` recreates the
   three tables, their indexes, functions and triggers, with no row.
   No stored policy document is converted (ADR-0146 item 1).
4. **A query with no caller goes.** `ListProjectsByMember` and its
   sqlc output are deleted. A scan of the platform's queries found no
   other without a caller.
5. **A test that fed the engine is rewritten onto the policy service
   or deleted with a reason.** Tests of the engine's own behaviour, its
   persistence, its cache and listener and `CheckCurrent`, are deleted
   with it. A test of a platform behaviour keeps what it asserted and
   no longer starts the engine; where it asserted an engine policy's
   effect, the policy service's own suite and the `policyservice`-tagged
   suite hold that boundary.
6. **The documents say so.** [authz.md](../authz.md),
   [deployment-needs.md](../deployment-needs.md),
   [overview.md](../overview.md) and the documents that named the
   engine state it as removed, and ADR-0003's status says what this
   supersedes.

## Consequences

- One process fewer to run, probe, scale and secure, and one gRPC
  contract fewer to keep. The platform's go.mod drops the `otelgrpc`
  instrumentation the engine alone used.
- Rolling back past this change needs the engine's code, and the down
  migration restores the tables empty, so a deployment that went back
  would hold no policy it had before. ADR-0146 item 1 already refused
  to convert one.
- The convention documents that describe `proto/` and `buf` for the
  platform, among them `stack-go.md`, describe a layout the platform no
  longer has. They are the PM's to revise.

## Signs the decision was wrong

- A deployment that runs a release before this one, and the policy
  service, finds an organization's earlier policies are wanted and
  cannot be read, because the tables are gone and the down migration
  restores no row.
- A caller that needs a decision before the policy service is
  reachable, such as a start-up path, is found and was served by the
  engine alone.
- A second gRPC surface of the platform is decided, and the removed
  `buf` configuration has to be rebuilt.
