-- A recovery that signed nothing has no certificate to keep, so it
-- refuses the restored NOT NULL rather than being deleted here.
ALTER TABLE agent_recoveries
    DROP CONSTRAINT agent_recoveries_chain,
    ADD CONSTRAINT agent_recoveries_chain
        CHECK (issuer_pem IS NOT NULL AND server_root_pem IS NOT NULL) NOT VALID,
    ALTER COLUMN certificate_pem SET NOT NULL;
