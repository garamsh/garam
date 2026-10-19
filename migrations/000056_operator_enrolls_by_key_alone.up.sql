-- An operator enters by a key alone (ADR-0117): its one-time
-- enrollment token and its shared-secret credential go, and every
-- operator token is one a key was exchanged for. A supervisor's
-- credential and tokens are separate tables (000042) and stay.
--
-- Each of these refuses rather than drops a row: a deployment still
-- holding a token, a credential, a token minted under one, or an audit
-- event of either kind fails here, naming which, rather than losing it
-- in a drop.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM operator_enrollment_tokens) THEN
        RAISE EXCEPTION 'operator_enrollment_tokens holds a row: an operator enrolls by key alone (ADR-0117)';
    END IF;
    IF EXISTS (SELECT 1 FROM operator_credentials) THEN
        RAISE EXCEPTION 'operator_credentials holds a row: an operator exchanges a key alone (ADR-0117)';
    END IF;
    IF EXISTS (SELECT 1 FROM operator_tokens WHERE credential_id IS NOT NULL) THEN
        RAISE EXCEPTION 'operator_tokens holds a token minted under a credential (ADR-0117)';
    END IF;
    IF EXISTS (SELECT 1 FROM audit_credential_events
               WHERE credential IN ('operator_credential', 'operator_enrollment_token')) THEN
        RAISE EXCEPTION 'audit_credential_events holds an operator credential or enrollment token event (ADR-0117)';
    END IF;
END
$$;

ALTER TABLE operator_tokens
    DROP CONSTRAINT operator_tokens_one_issuance,
    DROP COLUMN credential_id,
    ALTER COLUMN key_id SET NOT NULL;

DROP TABLE operator_credentials;
DROP TABLE operator_enrollment_tokens;

ALTER TABLE audit_credential_events
    DROP CONSTRAINT audit_credential_events_credential,
    ADD CONSTRAINT audit_credential_events_credential CHECK (credential IN (
        'operator_certificate', 'supervisor_certificate', 'supervisor_credential', 'agent_token'));
