-- Restores the fence to certificates alone, as 000029 defined it, before
-- the table it now reads goes.
CREATE OR REPLACE FUNCTION agent_credential_current(
    p_agent_grn text, p_serial text, p_not_before timestamptz, p_not_after timestamptz
) RETURNS boolean
LANGUAGE sql STABLE AS $$
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

DROP TABLE IF EXISTS agent_tokens;
