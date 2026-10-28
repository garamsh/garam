-- An organization delegates bounded execution authority to a hosted
-- operator, and a member hands that operator one request-bound
-- authority at a time (ADR-0078). Both tables hold authority metadata
-- only: which principals, operations, targets and digest, never a
-- desired runtime value.
--
-- A grant is immutable once written. Replacing one ends it and writes
-- another under a new identifier, so an operation authorized under the
-- first never becomes valid again under the second. A row records the
-- whole write that created it — request identifier, the grant it
-- replaced, the actor and the terms — so a replay is recognised only
-- when it is the same write. Ended grants stay as tombstones: their
-- request identifiers are what refuse a replayed write after a
-- withdrawal, and nothing here sweeps them. Deleting the
-- organization or the hosted operator removes them, and everything
-- minted under them, by the cascades below.
CREATE TABLE operator_delegations (
    id           text PRIMARY KEY,
    org_grn      text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    operator_grn text NOT NULL REFERENCES operators (grn) ON DELETE CASCADE,
    request_id   text NOT NULL,
    replaces     text,
    operations   text[] NOT NULL,
    expires_at   timestamptz NOT NULL,
    granted_by   text NOT NULL,
    granted_at   timestamptz NOT NULL,
    ended_at     timestamptz,

    CONSTRAINT operator_delegations_request UNIQUE (org_grn, operator_grn, request_id),
    CONSTRAINT operator_delegations_ownership
        CHECK (split_part(operator_grn, ':', 4) = 'operator'
           AND split_part(operator_grn, ':', 2) = split_part(org_grn, ':', 5)),
    CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 3
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate'])
);

-- At most one current grant per hosted operator. A grant write locks
-- this row before replacing it, so two writers holding one view cannot
-- both replace it.
CREATE UNIQUE INDEX operator_delegations_current_idx
    ON operator_delegations (org_grn, operator_grn) WHERE ended_at IS NULL;

-- The controllers a grant may act on. A deleted controller leaves every
-- grant naming it by the cascade, so registering the same identifier
-- again revives nothing.
CREATE TABLE operator_delegation_controllers (
    delegation_id  text NOT NULL REFERENCES operator_delegations (id) ON DELETE CASCADE,
    controller_grn text NOT NULL REFERENCES operators (grn) ON DELETE CASCADE,
    PRIMARY KEY (delegation_id, controller_grn)
);

CREATE INDEX operator_delegation_controllers_controller_idx
    ON operator_delegation_controllers (controller_grn);

-- One row per logical operation, keyed by the caller's request
-- identifier within an organization and audience. The short token is
-- replaced on remint and only its hash is stored; the row outlives it
-- as the durable reference a later activation rechecks and as replay
-- evidence. The actor carries no foreign key: deleting a user must not
-- free its request identifiers for another actor, and current
-- authorization is what refuses the deleted user's operations. The
-- assignment snapshot binds a configuration change to the placement it
-- was authorized for.
CREATE TABLE operation_authorities (
    ref              text PRIMARY KEY,
    org_grn          text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    audience_grn     text NOT NULL REFERENCES operators (grn) ON DELETE CASCADE,
    request_id       text NOT NULL,
    delegation_id    text NOT NULL REFERENCES operator_delegations (id) ON DELETE CASCADE,
    actor_grn        text NOT NULL,
    operation        text NOT NULL,
    target_grn       text NOT NULL,
    assignee_grn     text,
    assignment_epoch text,
    body_sha256      text NOT NULL,
    token_hash       text NOT NULL UNIQUE,
    token_expires_at timestamptz NOT NULL,
    created_at       timestamptz NOT NULL,

    CONSTRAINT operation_authorities_request UNIQUE (org_grn, audience_grn, request_id),
    CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure')),
    CONSTRAINT operation_authorities_assignment
        CHECK ((operation = 'agent:configure') = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)),
    CONSTRAINT operation_authorities_digest CHECK (body_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX operation_authorities_delegation_idx ON operation_authorities (delegation_id);
