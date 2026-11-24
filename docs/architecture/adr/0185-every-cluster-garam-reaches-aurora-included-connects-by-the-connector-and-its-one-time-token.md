# ADR 0185: Every cluster garam reaches, aurora's included, connects by the connector and its one-time token

> Status: accepted; amended by
> [ADR-0192](./0192-every-system-that-connects-to-garam-is-an-app-installed-in-an-organization-and-each-running-copy-is-an-instance-with-its-own-key.md),
> which connects every cluster as an instance of the connector app
> Date: 2026-11-24

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2054, for which the Garam PM reserved this number. garam
is read at `2fdfe7f8f0c6a137df6a46a24257ccf04c0ce1d5`, and a claim
below that names no revision holds for it.

## Context

ADR-0184 decides how a user's own cluster connects to garam:

- a registration token answered once (decision 3);
- a key the connector generates in its own process, which never leaves
  the cluster (decision 4);
- rotation the cluster does itself under its current key's signature
  (decision 6).

It speaks of "a user's own cluster" and does not say which clusters
take that path. ADR-0184's rejected alternatives name an issued key for
a cluster, the way an operator holds one, and refuse it.

garam's own deployment on aurora reaches garam today by that rejected
shape. [deployment-contract.md](../deployment-contract.md) §Operator
key and enrollment rollout records that aurora's operator
"authenticates by key" (aurora#664). That key is the PEM
`issueOperatorKey` generates in garam and answers once (ADR-0102,
ADR-0117). The owner describes the practice: a person issues a PEM,
a client id and a token URL in the console, one per process (the
manager and the control service), and places them by hand in a
cluster Secret, since garam-agent-operator v0.2.0 removed the
enrollment token.

The owner's direction of 2026-11-24 (`garamsh/discussions` discussion
6, comment 19852665,
https://github.com/garamsh/discussions/discussions/6#discussioncomment-19852665):
cluster connection works the way GitHub's ARC and Actions runners and
Rancher's agent do. A person takes one registration token from the
console and gives it at install. With it the cluster registers
itself, makes its own key and renews it. No person moves a PEM. This is
the connection of goal 4 (the BYOC connector), and "our aurora cluster
also connects the same way".

## Decision

1. **Every cluster garam reaches connects by the connector and its
   one-time registration token**, as ADR-0184 decides for a user's
   cluster. That includes the cluster garam's own deployment runs on,
   aurora. There is no second path for a cluster garam's operators run.
   ADR-0184 is amended by reference: its "a user's own cluster" reads
   as any cluster garam reaches.
2. **garam issues no PEM for a cluster.** A cluster's key is generated
   in the connector's process and kept in one Secret beside it
   (ADR-0184 decision 4), and the cluster rotates it itself (decision
   6). No key garam generates is placed into a cluster's process by a
   person.
3. **How many processes connect, how long a key lives and how it is
   renewed are garam's design**, the owner's direction says. They are
   ADR-0184's decisions and garam-bot-operator's interface
   `interfaces/cluster-connection/v1alpha1/`.

## Consequences

- aurora moves off the hand-placed operator PEMs once the connector and
  garam's half of it (#2045 to #2050) are built. Until then its
  operator runs on the key it holds, and nothing here removes that
  path ahead of its replacement.
- The operator record and `issueOperatorKey` stay for any caller that
  is not a cluster. This ADR decides how a cluster connects, not
  whether operators remain. Retiring the issued operator key once no
  cluster uses it is a later decision.

## Signs the decision was wrong

- aurora needs something of garam a user's cluster does not, and the
  connector grows an aurora-only branch to carry it.
- Moving aurora to the connector leaves it without a capability the
  operator's delegation gave it, and the two paths have to run side by
  side indefinitely.

## Rejected alternatives

- **Keep aurora on the issued operator PEM and connect only users'
  clusters by token.** It is the practice the owner's direction
  replaces, and it keeps a key that passes through garam and a browser
  in the one cluster garam runs on.
- **Bring back the operator's enrollment token for aurora.** ADR-0117
  removed it, and ADR-0184 brought a one-time token back for a cluster
  only, so the cluster record is where it lives.
