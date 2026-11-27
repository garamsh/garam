# ADR 0193: The floor is held in an app's requested set and not in the schema, and an instance's last contact stays the app system's

> Status: accepted
> Date: 2026-11-28

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2082, for which the Garam PM reserved this number
(#2082, comment 7221506408).

## Context

[ADR-0192](./0192-every-system-that-connects-to-garam-is-an-app-installed-in-an-organization-and-each-running-copy-is-an-instance-with-its-own-key.md)
decision 6 names a floor of actions no app may request, among them
`iam:bot.host`. It holds the floor by a manifest check: an action of
the floor that lists `Iam::Installation` or `Iam::Instance` among its
principals is a problem, so the pull request declaring one fails.

Decision 10 of the same ADR has an installation hold `iam:bot.host`,
by a `permit` its owner writes on the installation. Cedar decides an
action only for the principal types its declaration lists, so
`iam:bot.host` has to list `Iam::Installation`. The manifest check
refuses exactly that declaration, so the two decisions cannot both be
built.

The check conflates two questions. Which principals may hold an action
is the schema's to say. Whether an app may request an action is the
floor's, and an app requests an action only in its requested set.

Decision 10 also puts an instance's last contact in the bot domain,
beside its reports. Last contact is the time an instance's token last
resolved, which the app system's token resolution writes for every
app's instances, bot hosts or not, and the console reads it on the
instance's page, which is the app system's.

## Decision

The floor is held where an app's requested actions are checked. An
action of the floor is refused in a requested set whatever principals
its declaration lists, and the manifests carry no check of the floor.
This supersedes ADR-0192 decision 6's sentence "The manifest check
refuses an action of that property that lists `Iam::Installation` or
`Iam::Instance` as a principal, so a later action of that property is
unrequestable from the pull request that declares it." The floor's
members, and the rest of decision 6, stand.

An instance's last contact stays in the app system, and the bot
domain reads it from there where it answers a report. This supersedes
the words "and last contact" in ADR-0192 decision 10's "Declarations,
their log, reports and last contact belong to garam's bot domain";
the rest of decision 10 stands.

## Consequences

- `iam:bot.host` lists `Iam::Installation` as its principal and its
  resource, and an owner's `permit` grants it to one installation.
- The floor is a list in the platform (`app.Floor`), read by the
  check every install and every requested action passes through, and
  a test refuses each of its members in a request.
- A later action of the floor no longer fails the pull request that
  declares it. It is unrequestable only once it is added to the list,
  so the list is part of the review of such a pull request.
- A report and its instance's last contact are two reads of two
  stores and not one row, so an answer can pair them from different
  moments. Both are display data (ADR-0188).

## Signs the decision was wrong

- An app's requested set reaches a grant by some path that does not
  pass the requested-set check, so the floor no longer holds.
- An action of the floor's property is declared and requested before
  anyone adds it to the list, which the manifest check would have
  stopped.
- A reader needs a report and the contact that preceded it as one
  view, and two reads keep answering them apart.
