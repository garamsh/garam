# ADR 0074: An operator carries a name and description, and the console gives it a page of its own

> Status: accepted
> Date: 2026-09-28

## Context

An operator's record holds its GRN, who registered it and when
([ADR-0045](./0045-an-organization-registers-an-operator.md)). The GRN's
last segment is supplied by whoever registers it, because it is the
subject a certificate already names or will name
([ADR-0032](./0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md)),
and the record is what that certificate authenticates on
([ADR-0070](./0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md)).
That identifier is the only thing the console could show an operator
by, and nothing about the record could change: `operators.md` said a
record carries nothing to change.

#1127, under the console direction #1122 records, asks for operators to
be named, described, inspected and removed without changing their
machine identities. #1131 is its metadata and management slice; what an
operator reports at runtime stays with #1127 and #1047, and re-minting
an enrollment token from the console stays with #1036.

The Operators section was one page: a list of cards, each with a delete
control, over the register form. The Teams and Permissions sections
already open a page per entry
([ADR-0072](./0072-the-console-presents-a-group-as-a-team-with-a-page-of-its-own.md),
[ADR-0073](./0073-the-console-manages-policies-and-their-attachments-under-permissions.md)).

## Decision

- **An operator carries a name and a description beside its GRN.**
  Both are the organization's words about the record and nothing else:
  the GRN stays the identity, the subject the certificate names and the
  key every other table joins on. A name is trimmed, one to 100
  characters, and not unique; a description is trimmed, up to 1000
  characters, and may be empty. Both must be text PostgreSQL stores,
  so NUL and invalid UTF-8 are refused as the caller's error. Migration
  000025 adds the columns and checks the same bounds.
- **A registration that names none is called by its identifier**,
  including every one made before this decision: 000025 fills the name
  from the GRN's last segment, and `registerOperator` takes the name
  and description as optional fields beside the identifier.
- **`updateOperator` changes the two and nothing else.** A field left
  out keeps its value. The statement writes the two columns of the
  one row, so the certificate lineage (ADR-0046), an unspent token
  (ADR-0064) and every definition naming the operator stand as they
  were.
- **Updating is an action of its own, `operator:update`**, which owners
  and admins hold and members do not. It grants nothing a registration
  does and withdraws nothing a delete does, so neither of those actions
  is the one to carry it.
- **`getOperator` answers one operator** on `operator:read`, as the
  list answers each entry.
- **The console lists operators by name and opens a page per operator.**
  Operators is a section with item pages: `…/operators/{operator}` is
  an operator's page. The list shows each name with the identifier
  under it, and the register form stays under the list, showing the
  token once. The operator's page shows its identity, its enrollment
  state, runtime as unknown, the definitions naming it with a link to
  them, a form for the name and description, and the delete control.
- **Operators has no create route.** An identifier is typed, and `new`
  is a valid one that registrations may already hold, so a static
  `…/operators/new` would take that operator's page. Every segment
  below `…/operators/` is an operator's, and the breadcrumb names it
  as one.
- **Where a name alone could be ambiguous, the identifier stands beside
  it.** Defining an agent chooses the operator by name, each option
  carrying its identifier; a definition shows its operator by name
  with the identifier under it. Where the operator list is refused to
  the caller, the form takes a typed identifier and definitions show
  identifiers, as before.
- **Enrollment is not presented as contact.** The page labels the state
  as enrollment and shows runtime as unknown, because garam holds no
  report from an operator it can show; nothing is inferred from
  enrollment.
- **The delete refusal is unchanged and routed to.** A definition naming
  the operator still refuses the delete (ADR-0045, ADR-0070). The page
  lists those definitions, links to them on the Agents section, and the
  confirmation says that deleting the registration stops the operator
  authenticating and does not stop work already running in its cluster.

## Consequences

- An organization can tell two operators apart by what it calls them
  without re-registering either, and renaming one leaves every
  certificate, token and definition naming it as it was.
- Two operators can share a name. Every place the console offers a
  choice or names a definition's operator carries the identifier, which
  is what the API stores and what refuses a collision.
- A raw insert into `operators` must now name the operator: the column
  is `NOT NULL` without a default, so the e2e seed writes one.
- The console reads the organization's agent definitions on an
  operator's page. A caller refused that list still reads the operator,
  with the definitions marked not checked.
- The operation that mints another enrollment token still has no
  console control; #1036 decides it.

## Rejected alternatives

- **Renaming the identifier.** It is the certificate's subject and the
  key the definitions, tokens and certificate lineage are joined on;
  changing it would change the machine identity the issue says must
  not move.
- **A unique name.** It would refuse an organization two operators it
  wants to call the same, while the identifier already provides
  uniqueness where the API needs it.
- **Authorizing the update on `operator:register`.** A member who may
  add an operator would then rename every existing one, and the
  grant would no longer read as what it does.
- **A create page at `…/operators/new`, as Teams and Permissions
  have.** It would shadow the page of an operator registered as `new`;
  refusing that identifier at the API would break a registration that
  is valid today.
- **A default of `''` for the name column.** An empty name is what the
  domain refuses, so the default would admit through a raw insert the
  row the API cannot write.
