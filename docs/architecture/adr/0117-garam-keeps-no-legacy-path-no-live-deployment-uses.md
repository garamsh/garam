# ADR 0117: Garam keeps no legacy path no live deployment uses

> Status: accepted
> Date: 2026-10-19

Drafted for #1593, for which the Garam PM reserved this number on
2026-10-19. The owner's direction of that day was that, garam being
still in development, everything outside the new structure goes. The
PM decided the scope, the order and the policy deletion on #1593 the
same day. garam is read at `67c8781842077d5992c76bde7519675ac9abc2d9`;
a claim below that names no revision holds for it.

## Context

garam kept paths alive for deployments that might still use them: a
policy archive beside activation, an operator enrollment token and a
shared-secret operator credential beside the operator key, garam's own
agent templates as read-only records, a legacy `operatorGrn` beside
`assigneeGrn`, and a legacy execution mode with a staged cut-over out
of it. The QA inventory in #1592 read aurora's database, the one live
deployment, read-only:

- no policy of any status, and so none archived;
- no garam template;
- one operator, holding a certificate, and no key, shared-secret
  credential or enrollment token in use;
- one agent definition in legacy execution mode, with no cut-over
  started.

#1592 also called that definition dead, and that was wrong. At
2026-10-19T00:44:37Z aurora's operator minted a token for
`grn:garam:default:agent:144460a71649d1a1`, the definition it names,
and that agent is aurora's one running agent, placed from garam's
definitions through the operator's poller (#1593, the PM's rescoping
comment). On aurora, legacy mode is the live path.

Each kept path costs its tests, its contract text and every reader who
has to learn which half of a pair is the current one.

## Decision

**garam keeps no legacy path that no live deployment uses.** Each path
below is removed by a pull request of #1593, in this order, and each
pull request removes the console's callers of what it removes in the
same change.

1. **The policy archive is replaced by deletion.** `archivePolicy` and
   the `archived` status go, and `deletePolicy`
   (`DELETE /orgs/{org}/policies/{policy}`, on `policy:delete`) removes
   a policy with its versions and its attachments in one statement,
   which holds the policy's row lock. It refuses nothing on
   attachments.
   [ADR-0031](./0031-detaching-an-active-policys-last-attachment-is-refused.md)'s
   refusal stays, and its reason is preserved: removing an active
   policy's last attachment would widen it to the whole organization,
   while deleting the policy leaves nothing of it to widen. Its
   message names deletion where it named archiving. The status check
   stops admitting `archived` (migration 000055), so a deployment still
   holding an archived policy fails the migration rather than keep a
   state nothing reads.
2. **The operator enrollment token and the operator shared-secret
   credential go.** `mintOperatorEnrollment`, `enrollOperator`,
   `reissueOperatorCredential`, the token and credential
   `registerOperator` answers, and the stores behind them. A new
   operator then enters only by a key (`issueOperatorKey`,
   private_key_jwt under `machine-token.v2`). The supervisor's
   shared-secret exchange and `machine-token.v1` stay: supervisors use
   them, and agent tokens are minted under that contract. The
   certificate path stays too: aurora's operator authenticates only by
   its certificate until its key is issued and #664 rolls out (#1494).
   This removal does not wait for
   [ADR-0102](./0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)'s
   seven days without old authentication: the cohort it would measure
   is empty on the one live deployment.
3. **garam's retired agent templates go.** `listAgentTemplates`,
   `getAgentTemplate`, the `AgentTemplate` schema and the
   `agent_templates` table, after #1588 removes the console that reads
   them. The control service's templates, and the
   `agent-template:read` action they are read under, stay.
4. **`AgentAssignment.operatorGrn` goes**, after #1588 stops reading
   it. `assigneeGrn` names the holder, operator or supervisor.
5. **Legacy execution mode, the staged cut-over, `garam
   cutover-inventory` and legacy definitions go together**, after
   #1594 moves aurora's agent off them. That agent leaves legacy mode
   as a new managed agent, not by a cut-over: aurora deploys the
   operator's control service, a managed agent is created through it,
   the memory is copied with `garam copy-memory` from the old agent's
   state volume, the new agent is accepted, and only then is the old
   definition retired. Its GRN changes. Its rows are deleted only
   after the new agent is accepted and the PM holds a read-only
   `pg_dump --data-only` of them, kept private, since the deletion
   cannot otherwise be undone.

This supersedes:

- [ADR-0031](./0031-detaching-an-active-policys-last-attachment-is-refused.md)
  in part: archiving as the remedy its refusal names, and the archived
  policy whose last attachment detaches freely;
- [ADR-0073](./0073-the-console-manages-policies-and-their-attachments-under-permissions.md)
  in part: the console's archive and its "no delete";
- [ADR-0102](./0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
  in part: the operator shared-secret cohort it keeps under
  `machine-token.v1`, and the removal gate for that cohort;
- [ADR-0064](./0064-an-operator-enrols-with-a-one-time-token-and-keeps-its-key-in-its-own-cluster.md),
  in what ADR-0100 left of it;
- [ADR-0088](./0088-garams-agent-templates-are-retired-read-only-and-not-imported.md),
  and
  [ADR-0075](./0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)
  in part: the read of garam's own templates;
- [ADR-0082](./0082-a-standalone-host-runs-its-agent-under-a-supervisor-an-administrator-approves-by-its-keys.md)
  in part: the browser's legacy `operatorGrn`;
- [ADR-0086](./0086-a-legacy-agent-cuts-over-through-stages-garam-records-under-its-execution-lock.md)
  and
  [ADR-0090](./0090-garam-accepts-no-new-agent-definition-and-keeps-the-ones-it-holds.md);
- [ADR-0077](./0077-execution-definitions-move-to-the-operator-control-service-and-each-agent-cuts-over-alone.md)
  in part: each legacy agent cutting over alone;
- [ADR-0080](./0080-an-agents-execution-row-is-the-one-lock-its-writes-and-replacements-share.md)
  in part: the legacy mode of an agent's execution row;
- [ADR-0084](./0084-the-adapter-activates-its-generation-and-reports-runtime-status-under-a-contract-garam-owns.md)
  in part: the adapter's legacy mode.

[ADR-0002](./0002-iam-style-policy-model.md) stands: its versions stay
append-only and go only with the policy that owns them, which is what
a deletion does.
[ADR-0067](./0067-a-member-mints-a-second-enrollment-token-where-the-operator-holds-no-certificate.md)
is already superseded by ADR-0100.
[ADR-0078](./0078-a-lifecycle-authority-activates-a-runtime-generation-and-old-generations-and-lineages-are-fenced.md)
stands: its generation fence decides nothing about legacy mode or the
cut-over.

## Consequences

- An organization retires a policy in one act, and nothing in garam
  describes a state no policy can be in.
- A new operator has one way in, and an operator holding neither a key
  nor a certificate cannot authenticate.
- Until #1594 lands, legacy mode and the cut-over are described as
  live, with their removal named as pending, and aurora's agent runs
  on them unchanged.
- A deployment other than aurora that still uses a removed path loses
  it with no deprecation window. garam has no such deployment it knows
  of, and promises none across versions
  ([ADR-0049](./0049-neither-published-specification-promises-anything-across-versions.md)).
- Deleting a policy cannot be undone, as archiving could, and the
  console asks for the policy's name before it sends the delete.

## Signs the decision was wrong

- A deployment turns up that authenticates by a removed path, as
  aurora's agent turned out to run on legacy mode: the inventory
  measured what was stored rather than what was used, and the removal
  wanted the authentication evidence of every path, read before it
  shipped.
- An administrator deletes a policy meaning to pause it: retirement
  wanted a reversible state, and a deactivation back to draft was the
  shape.
- Aurora's agent loses memory or identity in its move to a managed
  agent: the cut-over this removes was the safer path, and legacy mode
  wanted to stay until a cut-over had run.
