-- A recovery keeps the chain its certificate was signed under, so a
-- retry answers the issuer and server root the signer produced rather
-- than ones read later (#1222).
--
-- The check is NOT VALID: every new write carries both, and a recovery
-- recorded before this migration keeps neither, since nothing here
-- knows what it was signed under. No row is rewritten.
ALTER TABLE agent_recoveries
    ADD COLUMN issuer_pem      text,
    ADD COLUMN server_root_pem text,
    ADD CONSTRAINT agent_recoveries_chain
        CHECK (issuer_pem IS NOT NULL AND server_root_pem IS NOT NULL) NOT VALID;
