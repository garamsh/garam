-- Which process of an agent may act, and with which credential lineage
-- (ADR-0078, ADR-0080).
--
-- Every agent has exactly one execution row. It is the per-agent mode a
-- caller cannot choose and the one lock that orders an activation, a
-- reassignment, a recovery or a deactivation against every protected
-- write: a writer takes it FOR SHARE through agent_execution_admit, and
-- whatever replaces the execution takes it FOR UPDATE, both inside
-- their own local transaction. A row that did not exist could not be
-- locked, so legacy agents get one too.
--
-- Lock order is agent_executions before agent_assignments in every
-- transaction that takes both.
CREATE TABLE agent_executions (
    agent_grn            text PRIMARY KEY REFERENCES agents (grn) ON DELETE CASCADE,
    mode                 text NOT NULL DEFAULT 'legacy' CHECK (mode IN ('legacy', 'fenced')),
    -- NULL is the legacy cohort: every certificate the agent's
    -- organization authority signed for it before a recovery.
    lineage              uuid,
    -- The activation whose token admits writes now, if any.
    activation_id        uuid,
    token_hash           bytea,
    -- The most recent activation, current or ended: what the next
    -- activation must name as the one it replaces.
    latest_activation_id uuid,
    updated_at           timestamptz NOT NULL,

    CONSTRAINT agent_executions_token_is_whole
        CHECK ((activation_id IS NULL) = (token_hash IS NULL)),
    CONSTRAINT agent_executions_only_fenced_activate
        CHECK (activation_id IS NULL OR mode = 'fenced'),
    CONSTRAINT agent_executions_current_is_latest
        CHECK (activation_id IS NULL OR activation_id = latest_activation_id)
);

INSERT INTO agent_executions (agent_grn, updated_at)
SELECT grn, now() FROM agents;

-- One row per activation, kept after it ends, so a retried request
-- answers what it did and a used generation is never activated again
-- within its lineage. A recovery moves to a new lineage, which is why
-- the lineage is part of that key.
CREATE TABLE agent_activations (
    id                     uuid PRIMARY KEY,
    agent_grn              text NOT NULL REFERENCES agent_executions (agent_grn) ON DELETE CASCADE,
    request_id             text NOT NULL CHECK (request_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    generation             text NOT NULL CHECK (generation ~ '^[0-9a-f]{32}$'),
    epoch                  integer NOT NULL CHECK (epoch > 0),
    lineage                uuid,
    audience_grn           text NOT NULL,
    grant_id               text NOT NULL,
    operation_ref          text,
    replaces_activation_id uuid REFERENCES agent_activations (id),
    token_version          bigint NOT NULL CHECK (token_version > 0),
    ended_reason           text CHECK (ended_reason IN ('superseded', 'deactivated', 'recovered', 'reassigned')),
    created_at             timestamptz NOT NULL,
    ended_at               timestamptz,

    CONSTRAINT agent_activations_end_is_whole
        CHECK ((ended_reason IS NULL) = (ended_at IS NULL)),
    CONSTRAINT agent_activations_request_once UNIQUE (agent_grn, request_id),
    CONSTRAINT agent_activations_generation_once
        UNIQUE NULLS NOT DISTINCT (agent_grn, generation, lineage)
);

ALTER TABLE agent_executions
    ADD CONSTRAINT agent_executions_activation_fk
        FOREIGN KEY (activation_id) REFERENCES agent_activations (id),
    ADD CONSTRAINT agent_executions_latest_activation_fk
        FOREIGN KEY (latest_activation_id) REFERENCES agent_activations (id);

-- The certificates of a tracked lineage. The legacy cohort records
-- none: every certificate of the agent belongs to it until a recovery
-- ends it.
CREATE TABLE agent_credentials (
    agent_grn text NOT NULL REFERENCES agent_executions (agent_grn) ON DELETE CASCADE,
    serial    text NOT NULL CHECK (serial ~ '^[0-9]{1,40}$'),
    lineage   uuid NOT NULL,
    not_after timestamptz NOT NULL,
    PRIMARY KEY (agent_grn, serial)
);

CREATE INDEX agent_credentials_lineage_idx ON agent_credentials (agent_grn, lineage);

-- A recovery, kept so a retry with the same request answers the
-- certificate it issued, and so one operation reference recovers once.
CREATE TABLE agent_recoveries (
    agent_grn       text NOT NULL REFERENCES agent_executions (agent_grn) ON DELETE CASCADE,
    request_id      text NOT NULL CHECK (request_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    operation_ref   text NOT NULL,
    body_sha256     bytea NOT NULL CHECK (octet_length(body_sha256) = 32),
    lineage         uuid NOT NULL,
    certificate_pem text NOT NULL,
    created_at      timestamptz NOT NULL,
    PRIMARY KEY (agent_grn, request_id),
    CONSTRAINT agent_recoveries_reference_once UNIQUE (operation_ref)
);

-- agent_credential_current answers whether a certificate of the agent
-- may act now: inside its validity at this instant, and in the
-- agent's current lineage. clock_timestamp() rather than now(), which
-- is when the transaction began and is stale after a lock wait.
CREATE FUNCTION agent_credential_current(
    p_agent_grn text, p_serial text, p_not_before timestamptz, p_not_after timestamptz
) RETURNS boolean
LANGUAGE sql VOLATILE AS $$
    SELECT p_not_before <= clock_timestamp()
       AND clock_timestamp() < p_not_after
       AND EXISTS (
           SELECT 1 FROM agent_executions e
           WHERE e.agent_grn = p_agent_grn
             AND (e.lineage IS NULL OR EXISTS (
                 SELECT 1 FROM agent_credentials c
                 WHERE c.agent_grn = e.agent_grn
                   AND c.serial = p_serial
                   AND c.lineage = e.lineage)))
$$;

-- agent_execution_admit is the one surface another domain reaches the
-- execution fence through (ADR-0080). A caller runs it first inside
-- the local transaction of its own write. It holds the execution row
-- FOR SHARE until that transaction ends, so a replacement either waits
-- for the write to commit or commits first and the write is refused.
--
-- It answers 'admitted' or the reason it refused: 'unknown_agent',
-- 'not_cut_over' (a token for an agent still in legacy mode),
-- 'token_required', 'token_stale' or 'credential_fenced'.
CREATE FUNCTION agent_execution_admit(
    p_agent_grn text, p_token_hash bytea, p_serial text,
    p_not_before timestamptz, p_not_after timestamptz
) RETURNS text
LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    v_mode text;
    v_token_hash bytea;
BEGIN
    SELECT mode, token_hash INTO v_mode, v_token_hash
    FROM agent_executions WHERE agent_grn = p_agent_grn
    FOR SHARE;
    IF NOT FOUND THEN
        RETURN 'unknown_agent';
    END IF;
    IF v_mode = 'legacy' THEN
        IF p_token_hash IS NOT NULL THEN
            RETURN 'not_cut_over';
        END IF;
    ELSIF p_token_hash IS NULL THEN
        RETURN 'token_required';
    ELSIF v_token_hash IS NULL OR v_token_hash <> p_token_hash THEN
        RETURN 'token_stale';
    END IF;
    IF NOT agent_credential_current(p_agent_grn, p_serial, p_not_before, p_not_after) THEN
        RETURN 'credential_fenced';
    END IF;
    RETURN 'admitted';
END
$$;
