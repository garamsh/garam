-- garam issues, renews and accepts no certificate of a machine caller
-- (ADR-0118, G4 of #1494). What only the certificate path wrote goes:
-- the operator certificate record, a managed agent's initial
-- certificate, a supervisor's renewals, the certificate serials of an
-- agent's lineage, and the certificate and key-fingerprint columns of
-- recoveries, supervisors and approvals. It is public material; none of
-- it is sealed.
--
-- certificate_authorities is not touched. Each of its rows holds an
-- authority's private key sealed under the key-encryption key, which
-- nothing can make again once dropped; whether to drop it is the
-- owner's decision, and until then the rows stay, unread.
--
-- A supervisor still holding a live certificate, or an unspent approval
-- binding a host's keys, is refused here rather than stranded: neither
-- can authenticate or be spent once this runs.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent_supervisors
               WHERE ended_at IS NULL AND not_after > now()) THEN
        RAISE EXCEPTION 'agent_supervisors holds a live certificate: garam accepts no certificate after this (ADR-0118)';
    END IF;
    IF EXISTS (SELECT 1 FROM agent_enrollments
               WHERE consumed_at IS NULL AND withdrawn_at IS NULL AND expires_at > now()
                 AND supervisor_key_sha256 IS NOT NULL) THEN
        RAISE EXCEPTION 'agent_enrollments holds an unspent approval binding host keys: garam signs no certificate after this (ADR-0118)';
    END IF;
END
$$;

DROP TABLE operator_certificates;
DROP TABLE agent_initial_certificates;
DROP TABLE agent_supervisor_renewals;

-- An agent's token is current by the agent_tokens row it rests on, so
-- the fence reads no certificate serial.
CREATE OR REPLACE FUNCTION agent_credential_current(
    p_agent_grn text, p_serial text, p_not_before timestamptz, p_not_after timestamptz
) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT p_not_before <= clock_timestamp()
       AND clock_timestamp() < p_not_after
       AND EXISTS (
           SELECT 1 FROM agent_executions e
           JOIN agent_tokens t ON t.agent_grn = e.agent_grn
           JOIN agent_assignments a
             ON a.agent_grn = t.agent_grn
            AND a.assignee_grn = t.assignee_grn
            AND a.epoch = t.epoch
           WHERE e.agent_grn = p_agent_grn
             AND t.serial = p_serial
             AND t.lineage IS NOT DISTINCT FROM e.lineage
             AND t.expires_at > clock_timestamp())
$$;

DROP TABLE agent_credentials;

ALTER TABLE agent_recoveries
    DROP CONSTRAINT agent_recoveries_chain,
    DROP COLUMN certificate_pem,
    DROP COLUMN issuer_pem,
    DROP COLUMN server_root_pem;

ALTER TABLE agent_supervisors
    DROP CONSTRAINT agent_supervisors_certificate_whole,
    DROP COLUMN serial,
    DROP COLUMN not_after;

ALTER TABLE agent_enrollments
    DROP CONSTRAINT agent_enrollments_consumption_is_whole,
    DROP CONSTRAINT agent_enrollments_direct_is_keyless,
    DROP CONSTRAINT agent_enrollments_keys_whole,
    DROP COLUMN supervisor_certificate_pem,
    DROP COLUMN agent_certificate_pem,
    DROP COLUMN issuer_pem,
    DROP COLUMN server_root_pem,
    DROP COLUMN supervisor_key_sha256,
    DROP COLUMN agent_key_sha256,
    ADD CONSTRAINT agent_enrollments_consumption_is_whole
        CHECK ((consumed_at IS NULL) = (request_sha256 IS NULL)
           AND (consumed_at IS NULL) = (epoch IS NULL)
           AND (consumed_at IS NULL) = (lineage IS NULL));
