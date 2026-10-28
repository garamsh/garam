-- The last time each machine caller authenticated by each path, which
-- the G3 gate reads to name the callers still on the certificate path
-- and the ones that have gone idle (ADR-0109). No request metric can
-- name an idle caller, and a label carrying a GRN is unbounded.
--
-- A row is one caller and one path. first_at and last_at bound when the
-- path was seen. A row for an exchange or a mint also records until
-- when the token it last issued is accepted, and last_refresh_at is the
-- last time the caller was issued another while that one was still
-- accepted. That is the only refresh the server can observe. No column
-- holds a credential, an assertion, a token or any part of one.
--
-- The tenant's rows go with the tenant: org_grn references the
-- organization and cascades. Nothing references the caller's own
-- registration, so a caller deleted while its organization remains
-- keeps its rows, which are the evidence that it stopped.
CREATE TABLE machine_authentication_evidence (
    caller_grn      text NOT NULL,
    path            text NOT NULL,
    org_grn         text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    first_at        timestamptz NOT NULL,
    last_at         timestamptz NOT NULL,
    issued_until    timestamptz,
    last_refresh_at timestamptz,

    PRIMARY KEY (caller_grn, path),
    CONSTRAINT machine_authentication_evidence_path CHECK (path IN (
        'certificate', 'token',
        'exchange_shared_secret', 'exchange_private_key_jwt', 'agent_mint', 'audience_token')),
    -- Only an exchange or a mint issues anything to refresh.
    CONSTRAINT machine_authentication_evidence_issuance CHECK (
        (path IN ('certificate', 'token') AND issued_until IS NULL AND last_refresh_at IS NULL)
        OR (path NOT IN ('certificate', 'token') AND issued_until IS NOT NULL)),
    CONSTRAINT machine_authentication_evidence_ownership
        CHECK (split_part(caller_grn, ':', 2) = split_part(org_grn, ':', 5))
);

-- An organization's deletion finds its rows through this.
CREATE INDEX machine_authentication_evidence_org_idx ON machine_authentication_evidence (org_grn);
