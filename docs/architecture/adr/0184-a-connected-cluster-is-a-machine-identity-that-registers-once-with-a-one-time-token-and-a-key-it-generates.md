# ADR 0184: A connected cluster is a machine identity that registers once with a one-time token and a key it generates

> Status: accepted
> Date: 2026-11-24

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2043, for which the Garam PM reserved this number. garam is
read at `6c3aaf69ed5a76bf4451353371f99a9d2a390222`, and a claim below
that names no revision holds for it.

## Context

The owner decided on 2026-11-24 (`garamsh/discussions` discussion 6,
comment 19835113, recorded in `garamsh/naru#373` §3) that a user
connects a cluster they own, EKS or any conformant cluster, by
installing a connector with a Helm chart. The connector dials out to
garam and registers with a one-time token. Its design is
`garamsh/garam-bot-operator` ADR 0087 to ADR 0092
(`garamsh/garam-bot-operator#437`). The wire between garam and the
connector is that repository's interface
`interfaces/cluster-connection/v1alpha1/`, which garam implements as the
server.

The same day, answering that design's questions (relayed by cupa), the
owner decided:

- a one-time registration token that lives one hour, is good for one
  attempt, and is stored as its hash;
- a key generated in the cluster;
- `private_key_jwt`;
- rotation signed by the current key;
- a new garam resource for the cluster, one per chart release, in one
  organization;
- this ADR, superseding ADR-0117 in part, by reference.

What garam holds today:

- **No one-time token.** ADR-0117 item 2 removed the operator enrollment
  token, which ADR-0064 had introduced for an operator that generates
  its own key, because no live deployment used it. An operator now
  enters only by a key `issueOperatorKey` generates in garam and answers
  once as PEM ([operators.md](../operators.md) §Issued PEM identity).
- **No cluster.** [overview.md](../overview.md) §Out of scope lists
  "Clusters as a resource an organization connects" as named and
  unplanned.
- **The exchange a key-holding caller uses**, `exchangeClientAssertion`
  under `machine-token.v2` (ADR-0102), and a token read on every request
  beside the record it rests on (ADR-0070, ADR-0100).

## Decision

1. **A connected cluster is a machine identity of its own,
   `grn:<org>/iam/cluster/<id>`.** It sits in `iam` with `operator`,
   `supervisor` and `bot`. garam mints the identifier, as it does a
   supervisor's, and answers it at registration, because the connector
   learns its identity there and is configured with none before it. The
   organization's name and description for the record are not its
   identity. It is not an operator: an operator's record carries
   delegation grants and a control endpoint, and nothing of either
   applies to a cluster.
2. **One cluster record belongs to one organization, and one connector
   release authenticates as one record.**
3. **A cluster registers once, by spending a one-time token, with a key
   it generated itself.** ADR-0117 item 2 is superseded in part: the
   one-time token returns, for a cluster only. An operator still enters
   only by an issued key.
   - The token is answered once, when a member creates the record, or
     when a member re-registers it under `iam:cluster.reissue`.
   - It carries at least 128 bits from a cryptographically secure
     source.
   - Registration is throttled per source address before the token is
     read, so a throttled request (429) spends no token.
   - It lives one hour and is good for one attempt: presenting it spends
     it whatever the outcome, and a spent, expired or unknown token gets
     one answer.
   - garam stores its hash only, and deletes that hash once the token is
     spent or expired.
   - The connector presents the token with the public JWK of an RSA key
     it generated. garam binds that key to the record and withdraws
     every key the record held before.
4. **Why the token returns: the cluster's private key never leaves the
   cluster.** An issued key exists in garam's process and in an answer
   to a browser. A key the connector generated exists only in the
   cluster's connector and in one Secret beside it. ADR-0117 removed the
   token because nothing used it, not because it was unsafe. A cluster
   is a caller that uses it.
5. **After registration a cluster is an ordinary key-holding caller.**
   It exchanges `private_key_jwt` assertions for one-hour tokens under
   `machine-token.v2`. The listener admits each token while the record
   and the key it rests on are live, read on every request.
6. **The cluster rotates its key itself, and the current key signs the
   rotation.** The call carries the cluster's token, the successor's
   public JWK, an authorization the current key signed naming the
   successor's thumbprint, and a proof the successor signed. The token
   alone rotates nothing, so a token that leaks cannot install another
   key. The planned rollover holds both keys live for its 24 hours, a
   third live key is refused, and repeating a rotation answers as the
   first did.
7. **Revocation is an organization's.** Withdrawing a key, deleting the
   record, or re-registering refuses the next request.
8. **garam stores no private key, no Secret value, no Kubernetes
   credential and no address of the cluster.** What it does store is
   garam-bot-operator ADR 0088 decision 8: the record, a registration
   token's hash, public keys, token hashes, the declarations placed on
   the cluster with their tombstones and how far those tombstones have
   been purged, and the cluster's latest report.
9. **Actions:** `iam:cluster.register`, `iam:cluster.read`,
   `iam:cluster.update`, `iam:cluster.reissue` and `iam:cluster.delete`,
   named action by action for the reason the operator's are. Which roles
   hold each is decided where they are built.
10. **A cluster's token reaches the cluster-connection routes, and the
    mint of the tokens of a bot placed on that cluster by a live
    declaration, and nothing else.**

## Consequences

- garam gains a resource, its browser routes, and a route on the machine
  listener that takes no token, for registration.
  [overview.md](../overview.md) names a token-less route as what leaves
  "a standalone host's enrollment and the token exchanges reachable to a
  caller that holds none". Registration joins them, behind its own rate
  limit, once it is built. Until then overview.md's sentence describes
  what is built, and its §Out of scope entry names the route.
- The cluster-connection interface's server suite is what holds garam's
  implementation to the wire, run in garam's CI.
- Re-registering is how a lost key is recovered, and nothing is read
  back.
- Whether a cluster's own bots get garam identities is not decided here.

## Signs the decision was wrong

- A cluster key lost and re-registered becomes routine, which would show
  the key belongs somewhere sturdier than one Secret.
- The operator record and the cluster record grow the same features one
  by one, which would show they were one kind.
- A connector that cannot reach garam within the token's hour, behind a
  proxy a user configures slowly, makes the hour a support cost.

## Rejected alternatives

- **An issued key for a cluster**, as an operator holds. It needs no
  token, but the private key passes through garam and a browser.
- **Registering a cluster as an operator.** Decision 1.
- **Trusting the cluster's ServiceAccount issuer as a federated
  identity.** It needs garam to reach the cluster's OIDC issuer, which a
  kind cluster or a private cluster does not offer.
