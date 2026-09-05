# ADR 0067: A member mints an operator a second enrollment token where it holds no certificate, and the refusal where it holds one is where that question stays open

> Status: accepted
> Date: 2026-09-05

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[ADR-0064](./0064-an-operator-enrols-with-a-one-time-token-and-keeps-its-key-in-its-own-cluster.md)
§Consequences left one item open and named it precisely: *a route
that mints a second token for an operator already registered*. It
recorded that losing a token means registering under another
identifier or running `garam issue-certificate`, that both work, and
that such a route is what would remove the out-of-band path entirely —
a decision about what a signed-in member may mint rather than a
handler to add. This supersedes that item. Nothing else in ADR-0064
moves.

**Both existing answers cost something the design was built to
avoid.** Registering under a second identifier leaves the first in the
list for good, because ADR-0045 offers no delete; #991 records that.
`garam issue-certificate` reads the key-encryption key, which is why
ADR-0015 leaves it out of band and why an owner has had to be in the
loop for every operator this project has stood up. ADR-0064's own
context calls that command a root-level subcommand no serving process
references.

**The question is not one question.** Whether a member may mint again
splits, and the halves are not equally hard.

- **The operator holds no certificate.** It never enrolled, or its
  token expired unused. Minting again here is what deleting the record
  and registering the same identifier would be, and a member may
  already register.
- **The operator already enrolled.** It holds a certificate. Minting
  here issues a *second credential path* to an identity that has one.
  That is closer to issuing another credential for an existing subject
  than to registering, and it is what someone reaches for after a
  compromise — an argument both ways, depending on whether garam can
  tell the two callers apart. It cannot.

**The ordering the refusal needs already exists.** #1031 landed the
three enrollment states and, inside `Store.Register`'s own
transaction, the rule that a certificate outranks a token, because a
certificate is the outcome a token exists to produce. That is exactly
what decides which half a request falls in.

## Decision

**A member mints an operator another enrollment token where that
operator holds no certificate.** One operation,
`mintOperatorEnrollment`, taking
`POST /orgs/{org}/operators/{operator}/enrollment`
on the browser-facing listener. It answers the token once, as
`registerOperator` does, and garam keeps only its hash.

**It is authorized exactly as registration is** — `operator:register`,
on the organization, through the same call. No action was added and no
role changed. The argument is that this grants nothing new: where no
certificate exists, minting again is the delete-and-register a member
may already perform, expressed once instead of as a dance nobody can
complete.

**The operator's previous token stops being spendable.** A re-mint
leaving two live tokens is two credential paths for one identity,
which is the half below by accident. Two things hold it, and the
second is what makes the first unnecessary to remember:
`MintOperatorEnrollment` upserts on the operator, so the row carries
over to the new hash; and `migrations/000024` puts a unique constraint
on `operator_grn`, so two rows for one operator cannot be stored at
all. The sweep inside that statement excludes the operator being
minted for, so the row it would delete and the row the upsert lands on
are never the same one: PostgreSQL 17 documentation §7.8.4 records that
sub-statements in `WITH` "cannot 'see' one another's effects on the
target tables" and that updating one row twice in a statement "is not
supported. Only one of the modifications takes place, but it is not
easy (and sometimes not possible) to reliably predict which one." Were
the delete to be the one that takes, the caller would be answered a
token with no row behind it.

**It is refused where the operator holds a certificate, and the
refusal says why.** `ErrOperatorAlreadyEnrolled` carries the sentence a
person reads: that the operator has already enrolled, that a second
token would be a second credential path to an identity that has one,
and that garam does not decide to issue it. A person meeting it learns
a decision was taken rather than that something failed. **That message
is where the undecided half stays visible**, and it is what will bring
it back if a real case appears.

**The refusal is decided in the store, beside the write and inside its
transaction, by the aggregate's own `EnrollmentState`.** The state is
derived from tables the write does not touch, so deciding it outside
the transaction would decide it on a row that may have moved — the
reason `Store.Register` reads back rather than assumes. It reuses that
derivation rather than restating it in SQL: a certificate outranks a
token in one place, or it eventually does so in two ways.

## What this does not decide

- **Whether a member may mint for an operator that already
  enrolled.** It stays open, exactly as ADR-0064 left it, and the
  refusal above is where it is now visible. It has no forcing case,
  and garam cannot tell a legitimate caller from a compromised one.
  Deciding it as a side effect of a usability complaint is the shape
  refused in #1022, where conforming to a layout rule would have
  settled a question ADR-0051 reserved. What replaces a certificate an
  operator still holds is a renewal (ADR-0046); what recovers one it
  has lost is the out-of-band command (ADR-0032).
- **A delete for an operator record.** ADR-0045 stands. This removes
  the reason the missing delete hurt — a lost token no longer forces a
  second identifier — without offering one.
- **What enrolling does once a token is spent.** Unchanged: the
  operator generates its key pair in its own cluster, garam signs a
  key it never holds, and the token is spent by the attempt
  (ADR-0064).
- **Whether the console gains a control for this.** It has none, and
  this ADR does not add one; #1036 is where that is decided. The
  operation is reachable by an API caller alone until then.
- **The rate of the operation.** ADR-0064 §Consequences already
  records that nothing bounds the enrollment route's, for reasons that
  hold here: the cost is one indexed statement, not an Argon2id
  verification (ADR-0030).

## Consequences

- **A lost token no longer costs an identifier or the owner's
  attention**, which is the whole of what #1027 measured. The list
  stops accumulating `field-one-2`, and the routine call for
  `garam issue-certificate` is gone — it stays the recovery for an
  operator that already holds a certificate, which is the half left
  undecided.
- **An operator holds at most one enrollment token, and the schema is
  what says so.** Before this, one-per-operator was a property of
  there being a single minter; now it is a constraint, so whatever
  mints next inherits it rather than having to remember it.
- **The undecided half is stated to the person who meets it** rather
  than only in this record. That is the one place a reader outside
  this repository learns of it.
- **The refusal is decided on what was committed when the transaction
  read.** A certificate adopted between that read and the commit
  leaves an operator holding a certificate and a live token, which is
  the combination the record already admits and reads as `enrolled`
  (ADR-0064 permits registering a GRN that already holds one, which
  mints a token beside it). The window is narrow and the state it
  produces is not new.
- **A token minted here is not picked up by a running operator.** The
  adapter's enroller stops polling after its first refused attempt
  (`gagent-operator@7f352e6:internal/garam/enroller.go:145`), and its
  refusal already tells a person to register the operator again
  (`:164`). A token that was lost before it was ever placed is a
  different path and is picked up, because nothing was presented and
  the poll is still running. Nothing here changes either; the console
  copy that would have to say so is #1036's, and this operation has no
  console control to say it on.
- **Nothing distinguishes the sweep's exclusion.** Removing the
  predicate leaves every test in this change passing, because
  PostgreSQL 17.11 resolved the overlap in favour of the insert on
  every run measured. The predicate is kept against what the
  documentation declines to promise rather than against an observed
  failure, and no test here can force the other resolution. What the
  suite does assert is the property the failure would break — that a
  token minted for an operator whose row was past its window spends.
  This is the class [README.md](../README.md) §What nothing detects
  already describes: the requirement is stated, the detection is
  absent.
- **`enrollOperator`, the renewal, and the out-of-band command are
  untouched.** So is what a spent token does.

## Rejected alternatives

- **Deciding both halves.** It is the question ADR-0064 declined in
  one sentence, and answering it because a usability issue reached it
  is what #1022 refused. Nothing forces it, and garam has no way to
  tell a member recovering from a compromise from the compromise.
- **Refusing both halves and keeping the out-of-band path.** It is the
  status quo, and it leaves the owner in the loop for a case that
  grants nothing new — the one argument for the accepted half is that
  it is already permitted.
- **A delete for the operator record, so registering again is the
  re-mint.** It expresses the same permission with a route that looks
  like revoking an operator while revoking nothing, which is what
  ADR-0045 declines until revocation is decided.
- **Deciding the refusal in the handler.** Fewer moving parts, and it
  reads the state through a call that has already returned: a
  certificate adopted in between would be missed, which is the defect
  #1031 fixed inside `Store.Register` rather than introduce here.
- **Deriving the refusal in SQL beside the write.** One statement, no
  read-back. It is a second statement of "a certificate outranks a
  token", and two derivations of one rule are how they start
  disagreeing.
- **Invalidating the old token with a delete before the insert.** It
  works and it is a step each future minter has to remember. The
  unique constraint makes two rows unrepresentable instead, which is
  the same requirement met by the shape rather than by a procedure.
- **Answering the operator record beside the token, as
  `registerOperator` does.** The record does not change, and its
  derived state after a successful mint is `pending` by construction.
  `listOperators` already answers it for a caller that wants it.
- **A second action, `operator:enroll`, to authorize this.** It reads
  more precise and it grants a member something registration does not,
  for an operation whose entire argument is that it grants nothing
  new. It would also have to be added to a role, which is the change
  this deliberately does not make.
