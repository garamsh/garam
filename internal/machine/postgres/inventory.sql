-- The G3 inventory (ADR-0109): one row per machine caller garam holds a
-- registration or evidence for, with its cohort and the last time it
-- authenticated by each path. Run it as it stands:
--
--     psql "$DATABASE_URL" -f internal/machine/postgres/inventory.sql
--
-- A registered caller with every column NULL has not authenticated
-- since evidence began to be kept: an idle caller G3 has to name. One
-- whose registered column is false was deleted while its organization
-- remained, and its row is the evidence that it stopped.
-- last_certificate is a sighting an earlier release recorded: garam
-- accepts no certificate since ADR-0118, so nothing writes one now.
-- Nothing here reads a credential: the table holds none.
WITH callers AS (
    SELECT grn AS caller_grn,
           CASE WHEN control_endpoint IS NULL THEN 'operator' ELSE 'hosted_service' END AS cohort
    FROM operators
    UNION ALL
    SELECT grn, 'supervisor' FROM agent_supervisors WHERE ended_at IS NULL
    UNION ALL
    SELECT grn, 'agent' FROM agents
),
evidence AS (
    SELECT caller_grn,
           max(last_at) FILTER (WHERE path = 'certificate')              AS last_certificate,
           max(last_at) FILTER (WHERE path = 'token')                    AS last_token,
           max(last_at) FILTER (WHERE path = 'exchange_shared_secret')   AS last_shared_secret_exchange,
           max(last_at) FILTER (WHERE path = 'exchange_private_key_jwt') AS last_private_key_jwt_exchange,
           max(last_at) FILTER (WHERE path = 'agent_mint')               AS last_agent_mint,
           max(last_at) FILTER (WHERE path = 'audience_token')           AS last_audience_token,
           max(last_refresh_at)                                          AS last_refresh
    FROM machine_authentication_evidence
    GROUP BY caller_grn
)
SELECT COALESCE(c.cohort, split_part(ev.caller_grn, ':', 4)) AS cohort,
       COALESCE(c.caller_grn, ev.caller_grn)                  AS caller_grn,
       c.caller_grn IS NOT NULL                               AS registered,
       ev.last_certificate,
       ev.last_token,
       ev.last_shared_secret_exchange,
       ev.last_private_key_jwt_exchange,
       ev.last_agent_mint,
       ev.last_audience_token,
       ev.last_refresh
FROM callers c
FULL JOIN evidence ev ON ev.caller_grn = c.caller_grn
ORDER BY cohort, caller_grn;
