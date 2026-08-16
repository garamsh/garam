# ADR 0020: garam publishes images and a contract, not manifests

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

Nothing in this repository has ever been deployed. `deploy/local/`
holds a compose stack for one machine, and every architecture document
stops at the process boundary.

A cluster now exists, owned by a separate repository whose maintainer
reconciles it from git. That raises a question no ADR has answered:
what does garam hand over, and what does it keep?

Two facts from outside this repository decide part of it, checked
rather than assumed:

- The cluster is not in AWS. It is Talos on an on-premises hypervisor,
  so no node carries a role that authenticates to a container registry
  the way a managed cluster would.
- A registry token there expires in twelve hours, so a static pull
  secret written once breaks by the next morning.

The same two hold for a local Kubernetes: a developer's machine has
neither a node role nor a way to keep a token fresh.

## Decision

- **garam publishes two images and a contract. It does not publish
  manifests for any cluster it does not run.** The manifests that
  reconcile a cluster live in the repository that owns that cluster, so
  cluster state changes in one place.
- **Two images, because there are two runtimes.** One holds the Go
  binary, whose entry point takes a subcommand — `serve api`,
  `serve authz`, `serve machine`, `migrate` — so three processes and a
  job come from one build. The other holds the web application. Their
  names say which is which rather than naming the project, because a
  repository named for the project reads as the only one.
- **Tags are immutable.** A tag that can be repointed makes a manifest
  pinning it untrue: the reconciler reports synced while the running
  content changed, and the audit trail and the rollback target
  disappear together. This is cheap to set before the first push and
  meaningless to claim after.
- **Local development builds into the local cluster's own daemon and
  uses no registry.** Neither a laptop nor the target cluster can hold
  a registry credential that stays valid, and a development loop that
  depends on a cloud round trip is slower and fails in more ways than
  one that does not.
- **The contract garam publishes is: the ports each process listens on,
  the paths a probe may reach, every configuration value and whether it
  is secret, and how key material must arrive.** That last is not a
  preference — ADR-0015 requires a file readable only by its owner and
  forbids the environment, because a value there is inherited by child
  processes, readable at `/proc/<pid>/environ`, and captured in crash
  dumps.
- **Whoever consumes the contract may not substitute a static trust
  bundle for it.** The machine listener verifies a client certificate
  against the certificate authority of the organization that
  certificate names, looked up per connection. A pool holding every
  organization's authority would accept a certificate against a root
  that is not its own, which is the tenancy ADR-0014 rests on the trust
  root. There is nothing to mount and nothing to rotate.

## Consequences

- garam owes the accuracy of a contract to a repository it does not
  control, and a change that falsifies it is a change that breaks
  someone else's deployment silently. It belongs beside the code rather
  than in a message.
- The image holding the Go binary is also the image holding the
  key-encryption key that seals every organization's certificate
  authority. It is built on a base that carries a shell, which is a
  class of exposure worth removing separately from this decision.
- Two images means two repositories and two lifecycle policies, and the
  single repository that exists today is named for the project rather
  than for either image.
- A local cluster and the real one diverge in how they get images. That
  divergence is deliberate, and it means a local success does not prove
  the registry path works — only that the images do.
- Nothing here decides how the cluster exposes anything. Which address
  each process answers on, and which of them leaves the cluster, is the
  other repository's to decide against this contract.

## Rejected alternatives

- **One image serving both runtimes.** A Go binary and a Node
  application in one layer set, selected by entry point. It halves the
  publishing work and doubles what a compromise of either reaches,
  including into the image that holds the key-encryption key.
- **Publishing manifests from here.** Convenient while one person holds
  both repositories, and it puts cluster state in two places the moment
  they are not the same person. The failure is not that both write —
  it is that neither can tell which write is authoritative.
- **A registry for local development.** Uniform with the real path, and
  it makes an offline machine unable to run the stack it is developing,
  in exchange for uniformity with a path that is blocked for unrelated
  reasons.
- **Mutable tags with a digest pinned in the manifest.** Correct, and
  it moves the burden to every consumer forever rather than setting one
  flag once while it is still free.
