-- A recovery under execution-fence.v2 replaces the lineage and signs
-- nothing (ADR-0100 §Recovery under hourly tokens): it records no
-- certificate and no chain. A row carries a certificate with its chain,
-- or neither; 000036's rows that kept a certificate and no chain stay
-- unchecked, as that migration left them.
ALTER TABLE agent_recoveries
    ALTER COLUMN certificate_pem DROP NOT NULL,
    DROP CONSTRAINT agent_recoveries_chain,
    ADD CONSTRAINT agent_recoveries_chain
        CHECK ((certificate_pem IS NULL AND issuer_pem IS NULL AND server_root_pem IS NULL)
            OR (certificate_pem IS NOT NULL AND issuer_pem IS NOT NULL AND server_root_pem IS NOT NULL)) NOT VALID;
