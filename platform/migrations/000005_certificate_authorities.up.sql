-- An organization signs its agents' client certificates with its own
-- certificate authority (ADR-0014), so tenancy holds in the trust root:
-- a certificate signed by one org's CA does not chain against another's.
-- The org owns one CA, hence the primary key.
--
-- sealed_key holds the CA private key encrypted with a key-encryption
-- key the process is given at boot; the key itself never reaches a
-- column, so a database dump alone signs nothing. A CA is written once
-- and never rewritten — rotation is a new CA, not an update — so the
-- row carries no updated_at.
CREATE TABLE certificate_authorities (
    org_grn     text PRIMARY KEY REFERENCES orgs (grn) ON DELETE CASCADE,
    certificate bytea NOT NULL,
    sealed_key  bytea NOT NULL,
    not_after   timestamptz NOT NULL,
    created_at  timestamptz NOT NULL
);
