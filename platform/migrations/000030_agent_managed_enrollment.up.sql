-- An agent the hosted control service creates under a member's
-- agent:create reference, and the first certificate of its credential
-- lineage (ADR-0077, ADR-0078).
--
-- The creating transaction writes the agent, its execution row, its
-- first assignment and the row below together, so a failure between
-- them leaves nothing and a retry of the same reference answers the
-- same GRN. The execution row is born fenced with a lineage of its own
-- and no credential in it: agent_credential_current then refuses every
-- certificate of the agent until the initial issuance records one, so
-- a managed agent never enters the legacy cohort.
CREATE TABLE agent_managed_creations (
    agent_grn      text PRIMARY KEY REFERENCES agent_executions (agent_grn) ON DELETE CASCADE,
    audience_grn   text NOT NULL,
    controller_grn text NOT NULL,
    request_id     text NOT NULL CHECK (request_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    operation_ref  text NOT NULL,
    epoch          integer NOT NULL CHECK (epoch > 0),
    lineage        uuid NOT NULL,
    created_at     timestamptz NOT NULL,

    CONSTRAINT agent_managed_creations_reference_once UNIQUE (operation_ref),
    CONSTRAINT agent_managed_creations_request_once UNIQUE (audience_grn, request_id)
);

-- The first certificate a managed agent's lineage records, and the
-- public answer that issued it, kept so a retry of the same request
-- answers exactly what the first did. It holds no private key: the
-- certificate is signed over a key the requester keeps.
CREATE TABLE agent_initial_certificates (
    agent_grn       text PRIMARY KEY REFERENCES agent_managed_creations (agent_grn) ON DELETE CASCADE,
    request_id      text NOT NULL CHECK (request_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    operation_ref   text NOT NULL,
    epoch           integer NOT NULL CHECK (epoch > 0),
    request_sha256  bytea NOT NULL CHECK (octet_length(request_sha256) = 32),
    lineage         uuid NOT NULL,
    certificate_pem text NOT NULL,
    issuer_pem      text NOT NULL,
    server_root_pem text NOT NULL,
    not_after       timestamptz NOT NULL,
    created_at      timestamptz NOT NULL
);
