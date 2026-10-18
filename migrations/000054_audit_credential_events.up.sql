-- One event per machine credential garam issued, renewed or refused
-- to issue (#1560, ADR-0114): operator certificates and credentials,
-- supervisor certificates and credentials, operator enrollment tokens
-- and agent tokens. An event names the subject the credential is for,
-- what was decided, and for a certificate its serial and validity; for
-- a token or credential its expiry where it has one. No column holds a
-- key, a token, a credential, a certificate or any part or hash of
-- one.
--
-- replay is an answer that repeated what an earlier identical request
-- issued, and issued nothing new. reason is the refusal's errorx kind,
-- the class of the failure and never its text, which can carry what
-- the caller sent.
--
-- The tenant's rows go with the tenant (ADR-0106): org_grn references
-- the subject's organization and cascades. Nothing references the
-- subject itself, so a subject deleted while its organization remains
-- keeps its events.
CREATE TABLE audit_credential_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subject_grn text NOT NULL,
    org_grn     text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    credential  text NOT NULL,
    action      text NOT NULL,
    outcome     text NOT NULL,
    replay      boolean NOT NULL DEFAULT false,
    reason      text,
    serial      text CHECK (serial ~ '^[0-9]{1,40}$'),
    not_before  timestamptz,
    not_after   timestamptz,
    recorded_at timestamptz NOT NULL,

    CONSTRAINT audit_credential_events_credential CHECK (credential IN (
        'operator_certificate', 'operator_credential', 'operator_enrollment_token',
        'supervisor_certificate', 'supervisor_credential', 'agent_token')),
    CONSTRAINT audit_credential_events_action CHECK (action IN ('issue', 'renew')),
    CONSTRAINT audit_credential_events_outcome CHECK (
        (outcome = 'issued' AND reason IS NULL)
        OR (outcome = 'refused' AND reason IS NOT NULL AND NOT replay
            AND serial IS NULL AND not_before IS NULL AND not_after IS NULL)),
    CONSTRAINT audit_credential_events_ownership
        CHECK (split_part(subject_grn, ':', 2) = split_part(org_grn, ':', 5))
);

-- A subject's events are read newest first; an organization's deletion
-- finds its rows through the same index's leading column.
CREATE INDEX audit_credential_events_subject_idx
    ON audit_credential_events (org_grn, subject_grn, recorded_at DESC, id DESC);
