-- The short-lived tokens an agent authenticates with in place of a
-- certificate (ADR-0100). Its assignee — the operator holding it at the
-- current epoch — mints one, and garam stores only the token's hash.
--
-- A token names the assignee and the epoch it was minted at and the
-- credential lineage the agent's execution held then. Every request
-- presenting one reads it joined to the agent's assignment and
-- execution as they stand now, so a reassignment, which moves the
-- epoch, and a recovery, which replaces the lineage, each refuse it from
-- the next request on a connection already open too.
--
-- The serial is what the execution fence (ADR-0080) is handed in place
-- of a certificate's: `t` and hex, so it can never equal the decimal
-- serial of a certificate.
CREATE TABLE agent_tokens (
    token_hash   text PRIMARY KEY,
    serial       text NOT NULL UNIQUE CHECK (serial ~ '^t[0-9a-f]{32}$'),
    agent_grn    text NOT NULL REFERENCES agent_executions (agent_grn) ON DELETE CASCADE,
    assignee_grn text NOT NULL,
    epoch        integer NOT NULL CHECK (epoch > 0),
    lineage      uuid,
    audience_grn text,
    issued_at    timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,

    CONSTRAINT agent_tokens_audience_service_type
        CHECK (audience_grn IS NULL OR split_part(audience_grn, ':', 4) = 'operator')
);

-- The trim a mint runs reads an agent's tokens by audience and age,
-- and the sweep reads them by expiry.
CREATE INDEX agent_tokens_agent_grn_issued_at_idx ON agent_tokens (agent_grn, issued_at);

-- The fence answers a token's serial as it answers a certificate's: a
-- certificate is current while inside its validity and, once a lineage
-- exists, in it; a token is current while inside its validity, minted
-- at the agent's current assignee and epoch, and in the lineage the
-- execution holds now, NULL included.
CREATE OR REPLACE FUNCTION agent_credential_current(
    p_agent_grn text, p_serial text, p_not_before timestamptz, p_not_after timestamptz
) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT p_not_before <= clock_timestamp()
       AND clock_timestamp() < p_not_after
       AND EXISTS (
           SELECT 1 FROM agent_executions e
           WHERE e.agent_grn = p_agent_grn
             AND CASE WHEN left(p_serial, 1) = 't' THEN EXISTS (
                     SELECT 1 FROM agent_tokens t
                     JOIN agent_assignments a
                       ON a.agent_grn = t.agent_grn
                      AND a.assignee_grn = t.assignee_grn
                      AND a.epoch = t.epoch
                     WHERE t.agent_grn = e.agent_grn
                       AND t.serial = p_serial
                       AND t.lineage IS NOT DISTINCT FROM e.lineage
                       AND t.expires_at > clock_timestamp())
                 ELSE (e.lineage IS NULL OR EXISTS (
                     SELECT 1 FROM agent_credentials c
                     WHERE c.agent_grn = e.agent_grn
                       AND c.serial = p_serial
                       AND c.lineage = e.lineage))
                 END)
$$;
