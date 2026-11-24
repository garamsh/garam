# Clusters

What an organization records about a cluster it connected: a user's
own cluster, EKS or any conformant one, whose connector dials garam
and authenticates as that record
([ADR-0184](./adr/0184-a-connected-cluster-is-a-machine-identity-that-registers-once-with-a-one-time-token-and-a-key-it-generates.md)).
Implemented by `platform/internal/cluster`, which persists through
`platform/internal/cluster/postgres`. The wire between garam and the
connector is `garamsh/garam-bot-operator`'s interface
`interfaces/cluster-connection/v1alpha1/`, whose server garam owes.

| Type | Responsibility |
|------|----------------|
| `Cluster` | an organization's record of a connected cluster under a GRN garam minted: what the organization calls it and says it is for, who created it and when |
| `DetailsPatch` | a parsed name and description, each absent where the caller said nothing: what a creation starts with and what an update changes |
| `Store` | creates one, answers one by GRN, lists an organization's own, changes its name and description, and deletes one |

## Current decisions

- **A connected cluster is `grn:<org>/iam/cluster/<id>`, in one
  organization** (ADR-0184 decisions 1 and 2). garam mints the
  identifier, sixteen hex digits from a cryptographically secure
  source, because the connector learns its identity at registration
  and is configured with none before it. The ownership is held twice:
  `cluster.GRNOf` derives the GRN, and
  `platform/migrations/000082_clusters.up.sql` §clusters_grn_ownership
  refuses a row whose GRN names another organization or another type.
- **Every cluster garam reaches is such a record**, the one garam's
  own deployment runs on included, and garam issues no PEM for a
  cluster
  ([ADR-0185](./adr/0185-every-cluster-garam-reaches-aurora-included-connects-by-the-connector-and-its-one-time-token.md)).
- **The record carries what ADR-0184 decision 8 lists of it and
  nothing else**: the GRN, the organization, the name, the
  description, who created it and when. A name is not unique and not
  an identity, and one left out of a creation is the identifier. The
  bounds are `platform/api/openapi.yaml` §ClusterName and
  §ClusterDescription, which is authoritative for them, and 000082
  holds the same ones. The record follows its organization out.
- **The surface is the browser-facing listener, on the organization's
  own paths**: `createCluster`, `listClusters`, `getCluster`,
  `updateCluster` and `deleteCluster`. `platform/api/openapi.yaml`
  §cluster is authoritative for them.
- **Every cluster route is decided on the organization's GRN**, each
  on an action of its own (ADR-0184 decision 9): `iam:cluster.read`
  for the list and a read, `iam:cluster.register` to create,
  `iam:cluster.update` to rename and describe, `iam:cluster.delete`
  to delete. `iam:cluster.reissue` is defined for re-registering and
  no route takes it yet.
- **Which roles hold each**, decided where it was built (#2045): every
  role holds `iam:cluster.read`; the owner and the administrator hold
  `iam:cluster.register`, `.update`, `.reissue` and `.delete`. Each is
  named and none is granted as `iam:cluster.*`, for the operator's
  reason: a wildcard grants in advance an action nobody has decided.
  `platform/internal/org/role.go` §machineActions and
  `policy/schema/iam.cedarschema` are authoritative for the grants,
  and a test holds the two together.
- **`Iam::Cluster` is declared with `@caller("none")`**, so the GRN
  type exists in `lib/grn`'s catalogue and nothing feeds a cluster to
  the policy service: no decision names one while every route is
  decided on its organization.

## Rationale

- Why a cluster is its own machine identity and not an operator, and
  why its key never leaves the cluster: ADR-0184. Why aurora's cluster
  takes the same path: the owner's direction recorded in ADR-0185.
- Why every route is decided on the organization and the roles mirror
  the operator's: standing a machine identity up or taking it away is
  the administrative act registering and deleting an operator already
  are (ADR-0045, ADR-0070), and the PM settled the fork that way on
  #2045.

## Open questions

- Not yet built, each its own issue: the one-time registration token,
  registration, key rotation and revocation (#2046); the server
  conformance suite in CI (#2047); declarations placed on a cluster
  (#2048); the bot-token mint for a cluster (#2049); reports (#2050).
- The console pages for a cluster, built once the routes above are,
  in one issue filed after #2050.
