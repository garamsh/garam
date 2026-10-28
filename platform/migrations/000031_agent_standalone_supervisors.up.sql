-- A standalone host runs its agent under a supervisor: a machine
-- identity garam registers for exactly one agent on an administrator's
-- approval of the host's own keys (ADR-0082).

-- An agent is held by an operator or by its supervisor, so the column
-- names what holds it rather than one kind of holder. The pairing stays
-- inside one organization, as it always has (ADR-0015).
ALTER TABLE agent_assignments RENAME COLUMN operator_grn TO assignee_grn;
ALTER TABLE agent_assignments
    DROP CONSTRAINT agent_assignments_operator_service_type,
    DROP CONSTRAINT agent_assignments_same_org,
    ADD CONSTRAINT agent_assignments_assignee_service_type
        CHECK (split_part(assignee_grn, ':', 4) IN ('operator', 'supervisor')),
    ADD CONSTRAINT agent_assignments_same_org
        CHECK (split_part(assignee_grn, ':', 2) = split_part(agent_grn, ':', 2));

-- An administrator's approval of one host's request: the hash of a
-- bootstrap secret, the fingerprints of two public keys and a request
-- identifier, all of which the host generated and kept before asking.
-- garam never sees the secret until the host spends it, and an
-- identical approval retry answers the same binding. A new agent's GRN
-- and the supervisor's are minted at approval, so the enrollment that
-- spends it can only answer those two. A replacement names the
-- assignment it replaces, which the enrollment rechecks under the
-- execution lock.
--
-- The consumption columns record the public answer, so an identical
-- retry is answered without signing again. No private key is stored.
--
-- The agent column carries no foreign key: a new agent does not exist
-- until the enrollment writes it. Deleting the organization removes
-- every row.
CREATE TABLE agent_enrollments (
    id                         uuid PRIMARY KEY,
    org_grn                    text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    agent_grn                  text NOT NULL,
    supervisor_grn             text NOT NULL UNIQUE,
    host_name                  text NOT NULL CHECK (char_length(host_name) BETWEEN 1 AND 100),
    supervisor_key_sha256      bytea NOT NULL CHECK (octet_length(supervisor_key_sha256) = 32),
    agent_key_sha256           bytea NOT NULL CHECK (octet_length(agent_key_sha256) = 32),
    expected_assignee_grn      text,
    expected_epoch             integer CHECK (expected_epoch > 0),
    request_id                 text NOT NULL CHECK (request_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    approved_by                text NOT NULL,
    secret_sha256              bytea NOT NULL UNIQUE CHECK (octet_length(secret_sha256) = 32),
    expires_at                 timestamptz NOT NULL,
    created_at                 timestamptz NOT NULL,
    withdrawn_at               timestamptz,
    consumed_at                timestamptz,
    request_sha256             bytea CHECK (octet_length(request_sha256) = 32),
    epoch                      integer CHECK (epoch > 0),
    lineage                    uuid,
    supervisor_certificate_pem text,
    agent_certificate_pem      text,
    issuer_pem                 text,
    server_root_pem            text,

    CONSTRAINT agent_enrollments_request UNIQUE (org_grn, request_id),
    CONSTRAINT agent_enrollments_ownership
        CHECK (split_part(agent_grn, ':', 4) = 'agent'
           AND split_part(supervisor_grn, ':', 4) = 'supervisor'
           AND split_part(agent_grn, ':', 2) = split_part(org_grn, ':', 5)
           AND split_part(supervisor_grn, ':', 2) = split_part(org_grn, ':', 5)),
    CONSTRAINT agent_enrollments_replacement_is_whole
        CHECK ((expected_assignee_grn IS NULL) = (expected_epoch IS NULL)),
    CONSTRAINT agent_enrollments_consumption_is_whole
        CHECK ((consumed_at IS NULL) = (request_sha256 IS NULL)
           AND (consumed_at IS NULL) = (epoch IS NULL)
           AND (consumed_at IS NULL) = (lineage IS NULL)
           AND (consumed_at IS NULL) = (supervisor_certificate_pem IS NULL)
           AND (consumed_at IS NULL) = (agent_certificate_pem IS NULL)
           AND (consumed_at IS NULL) = (issuer_pem IS NULL)
           AND (consumed_at IS NULL) = (server_root_pem IS NULL)),
    CONSTRAINT agent_enrollments_spent_once
        CHECK (withdrawn_at IS NULL OR consumed_at IS NULL)
);

CREATE INDEX agent_enrollments_agent_idx ON agent_enrollments (agent_grn);

-- A supervisor garam registered for one agent, and the certificate it
-- last issued that supervisor: the record a renewal is measured
-- against. An ended registration admits nothing and is kept, so its
-- identifier is never registered again. At most one per agent is
-- current.
CREATE TABLE agent_supervisors (
    grn           text PRIMARY KEY,
    agent_grn     text NOT NULL REFERENCES agent_executions (agent_grn) ON DELETE CASCADE,
    enrollment_id uuid NOT NULL UNIQUE REFERENCES agent_enrollments (id) ON DELETE CASCADE,
    host_name     text NOT NULL CHECK (char_length(host_name) BETWEEN 1 AND 100),
    serial        text NOT NULL CHECK (serial ~ '^[0-9]{1,40}$'),
    not_after     timestamptz NOT NULL,
    registered_at timestamptz NOT NULL,
    ended_at      timestamptz,

    CONSTRAINT agent_supervisors_ownership
        CHECK (split_part(grn, ':', 4) = 'supervisor'
           AND split_part(grn, ':', 2) = split_part(agent_grn, ':', 2))
);

CREATE UNIQUE INDEX agent_supervisors_current_idx
    ON agent_supervisors (agent_grn) WHERE ended_at IS NULL;

-- A supervisor's renewal, keyed by the certificate it replaced, so a
-- retry presenting that certificate answers what the first attempt
-- issued. Only the latest renewal is replayable: the next one makes
-- this row's predecessor two steps behind the registration.
CREATE TABLE agent_supervisor_renewals (
    supervisor_grn     text NOT NULL REFERENCES agent_supervisors (grn) ON DELETE CASCADE,
    predecessor_serial text NOT NULL CHECK (predecessor_serial ~ '^[0-9]{1,40}$'),
    request_id         text NOT NULL CHECK (request_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    request_sha256     bytea NOT NULL CHECK (octet_length(request_sha256) = 32),
    serial             text NOT NULL CHECK (serial ~ '^[0-9]{1,40}$'),
    certificate_pem    text NOT NULL,
    issuer_pem         text NOT NULL,
    server_root_pem    text NOT NULL,
    not_after          timestamptz NOT NULL,
    created_at         timestamptz NOT NULL,
    PRIMARY KEY (supervisor_grn, predecessor_serial)
);
