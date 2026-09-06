# ADR 0046: An operator renews its certificate by presenting the one it holds

> Status: accepted
> Date: 2026-08-25

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives
- Errata

## Context

[ADR-0015](./0015-operator-identity.md) left an operator's own first
certificate out of band, and
[ADR-0032](./0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md)
shipped the command that mints it. Nothing renewed it. Every other
identity on the machine listener has a route:
[ADR-0040](./0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
§Consequences recorded the hole in as many words — "an operator's own
certificate still has no renewal path" — and #509 measured what it
costs. `GARAM_CERTIFICATE_LIFETIME` defaults to a day, so an operator's
credential expires daily, and with no route the out-of-band delivery is
a repeating obligation rather than a bootstrap: something outside garam
mints and carries a certificate to every operator, every day, forever.

Half of what a renewal needs is already built. ADR-0040 made both ends
read their pair per handshake, so a replaced certificate is presented on
the next connection with nothing restarted. A renewal has somewhere to
land, and nothing here adds a reload, a signal or a watch.

Two findings decided it, and neither is about ergonomics.

- **A scheduled writer would be the first workload principal in the
  cloud account with a write path to the key.** `garamsh/infra` read the
  account's key policy while weighing the delivery mechanism: exactly
  one principal holds `kms:GenerateDataKey`, and it is a human's SSO
  role. Depositing a fresh certificate on a schedule needs that
  statement beside `secretsmanager:PutSecretValue`, and their own
  `aws.md` records why no workload has it. A once-per-operator step
  needs no role at all — it is a value placed by the person who holds
  it, which is how the four values already in that account arrived.
- **At more than one customer, "the deployment carries it forever" is a
  capability nobody should hold.** It means a standing credential-minting
  capability in the vendor's account, exercised daily, for every tenant
  at once.

What makes this a security decision rather than a convenience is that
garam has no revocation. [ADR-0014](./0014-agent-authentication.md)
accepted expiry as the only one there is — no CRL, no responder — and
expiry is exactly what a renewal defers. A certificate that can renew
itself without limit is a permanent credential, and a stolen copy of it
renews as readily as the operator does.

## Decision

1. **An operator renews over the connection its current certificate
   authenticated**, on the machine listener: `POST
   /identity/certificate`. That listener verifies a client certificate
   per connection against the authority of the organization the
   certificate's own SAN names (`internal/machine/listener.go`), so it
   is the one surface where the caller is already proven.
2. **The route names no subject.** What is replaced is what was
   presented, so there is no identity a caller could ask for and nothing
   to authorize past the handshake. A caller that is not an operator is
   refused: an agent's certificate is admitted by the claim its operator
   made ([ADR-0033](./0033-an-operator-claims-the-agent-a-definition-mints.md))
   and renewed on the route ADR-0040 gave it, and nothing here changes
   that.
3. **An expired certificate cannot renew.** This is the bootstrap
   boundary. The handshake refuses such a certificate before any handler
   is reached, and the renewal rule states the same boundary as the
   empty end of its window, so reaching the rule any other way meets the
   same refusal. What follows an expired certificate is a mint out of
   band (ADR-0032) and nothing else.
4. **A renewal is admitted once two thirds of the presented
   certificate's validity has passed**, measured from the certificate
   itself rather than from a configured number, so the window holds
   whatever lifetime signed it. Both ends of that choice are load
   bearing: a certificate renewable from the moment it is issued has no
   lifetime worth configuring, and a window narrower than this would
   turn an operator polling on a slow cadence into an operator locked
   out. At the default lifetime the window is about eight hours.
5. **Only the certificate garam last issued for an operator may renew
   it.** garam records that certificate — one row per operator, holding
   its serial — and a renewal moves the record to the successor in the
   same statement that checks it. Every other certificate naming that
   operator stops being renewable at once. This is what keeps a stolen
   certificate from being permanent: the operator it was taken from
   renews, the copy loses the lineage, and it expires.
6. **The out-of-band mint takes the lineage.** `garam
   issue-certificate` records what it mints, so re-running it makes
   every certificate that already names the operator un-renewable. That
   is the recovery from a renewal answer nobody received and from a
   copy that renewed first, and it is the closest thing to a revocation
   garam has.
7. **The successor is signed before the record moves, and a caller that
   lost the lineage receives nothing.** The certificate it would have
   received is discarded inside the call, with the private key that
   exists nowhere else.
8. **The three refusals are told apart by kind, not by prose.** Too
   early is `failed_precondition` and means ask again; superseded is
   `already_exists` and means only a mint out of band recovers;
   expired and "not an operator" are `permission_denied`. A client acts
   on the kind.

## Consequences

- **Out-of-band delivery becomes a bootstrap.** It happens once per
  operator instead of once a day, which is what removes the standing
  grant §Context describes. How that first certificate reaches an
  operator is still open and is settled elsewhere; what changes here is
  how much has to be trusted to carry it.
- **A stolen certificate is bounded without anything being revoked.**
  It stays usable until it expires — no lifetime is shortened here and
  no certificate is withdrawn — but it renews only until the operator it
  was taken from renews. Whichever of the two loses the lineage is
  refused at its next renewal and stops connecting a lifetime later, so
  a theft that silences the operator is a theft the deployment sees.
- **This is not revocation, and it does not add one.** There is no CRL
  and no responder. A superseded certificate still authenticates for the
  rest of its life, and nothing here reports that one was superseded.
- **An operator identity is held by one process.** Two replicas sharing
  one certificate will both renew, one will lose the lineage, and it
  will be refused from then on. A deployment that wants two processes
  gives them two identities.
- **A renewal answer that never arrives costs a mint out of band.**
  garam keeps no private key, so it cannot answer twice, and the record
  has already moved. The recovery is the command, and it is the same
  act as the bootstrap.
- **The lifetime can now be lowered.** ADR-0040 left the default loose
  while rotation was new; with an operator renewing itself, the
  remaining obstacle to a shorter one is the deployment obligation
  `deployment-contract.md` states, not a manual step.
- **Not in scope, and not implied.** Revocation does not exist and this
  does not add it. The agent's certificate is unchanged. The enrolment
  ticket — how an operator's first certificate reaches it — is separate
  work, and this decision is what makes it a one-time path rather than a
  daily one. The record an organization keeps of an operator
  ([ADR-0045](./0045-an-organization-registers-an-operator.md)) is not
  that path and is not read here: an operator renews with what it holds,
  registered or not.
- **An operator that predates this has no certificate recorded**, and
  its first renewal adopts one. Nothing has been superseded where
  nothing was recorded, so no deployment has to be migrated.

## Rejected alternatives

- **Record the absence as deliberate: the deployment provisions and
  rotates operator credentials forever.** #509's first answer. It is
  defensible for a deployment whose operators are all its own, and it
  buys a standing write path to the key-encryption key in the cloud
  account, exercised daily, for every tenant — which is what §Context
  weighs and refuses.
- **Renewal with no lineage record.** The kubelet's shape and the
  smallest thing that answers #509. Where there is revocation it is
  enough; here it makes a stolen certificate permanent, which is the
  one property this decision exists to deny.
- **A ceiling on how long a lineage may be carried on** — re-bootstrap
  every so many days. It bounds a theft by a clock instead of by the
  operator's own renewal, and it brings back exactly the scheduled
  out-of-band writer, at a lower rate. The grant it needs is the same
  grant.
- **A subject in the path**, mirroring the agent's route. It lets a
  caller name an identity the design forbids it to name, so it buys an
  authorization check that exists only to refuse what the route should
  not have been able to express.
- **Keeping the predecessor renewable for a grace period after a
  successor is issued.** It would make a lost answer retryable, at the
  price of a second mechanism, a second stored fact, and a window in
  which two certificates renew. The lost answer already has a recovery,
  and it is one the deployment must be able to perform anyway.
- **The renewal on the browser API.** That surface authenticates a
  session and a user (ADR-0005); an operator has neither, and the
  certificate being replaced is not something a cookie proves.

## Errata

- 2026-08-26 — §Decision item 8 opens "The three refusals are told
  apart by kind" and then names **four** conditions: too early,
  superseded, expired, and "not an operator". There are four refusals
  in three kinds, because expired and "not an operator" share
  `permission_denied` — which is what `internal/operator/operator.go`
  declares (`ErrRenewalTooEarly`, `ErrSuperseded`,
  `ErrCertificateExpired`, `ErrNotAnOperator`) and what the sentence's
  own list says. The claim the item makes is unaffected and correct: a
  client acts on the kind, and there are three of those. What is wrong
  is the noun — "three refusals" where the three counts kinds. The same
  count was corrected in `docs/architecture/domains.md`, which had
  stated three refusals rather than four (#571).
- 2026-09-06 — §Context's "What makes this a security decision rather
  than a convenience is that garam has no revocation", and
  §Consequences' "This is not revocation, and it does not add one",
  were true when written and are now qualified:
  [ADR-0070](./0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md)
  gives an organization one, for an operator, by deleting the record
  its certificate authenticates on. **The decision here is unaffected
  and was measured to be**: ADR-0070 §Decision records that a caller
  holding the certificate garam has just replaced is still answered 200
  under the mechanism chosen, and that the alternative which would have
  answered 401 was refused on exactly that ground. So §Consequences' "A
  superseded certificate still authenticates for the rest of its life"
  stands, and the lineage rule is still what bounds a stolen
  certificate belonging to an operator the organization still wants.
  What deleting adds is an end for one it does not.
