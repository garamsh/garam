-- A v2 approval or supervisor holds no key or certificate to restore,
-- so the restored NOT NULLs refuse rather than delete such a row.
DROP TABLE supervisor_tokens;
DROP TABLE supervisor_credentials;

ALTER TABLE agent_supervisors
    DROP CONSTRAINT agent_supervisors_certificate_whole,
    ALTER COLUMN serial SET NOT NULL,
    ALTER COLUMN not_after SET NOT NULL;

ALTER TABLE agent_enrollments
    DROP CONSTRAINT agent_enrollments_consumption_is_whole,
    ADD CONSTRAINT agent_enrollments_consumption_is_whole
        CHECK ((consumed_at IS NULL) = (request_sha256 IS NULL)
           AND (consumed_at IS NULL) = (epoch IS NULL)
           AND (consumed_at IS NULL) = (lineage IS NULL)
           AND (consumed_at IS NULL) = (supervisor_certificate_pem IS NULL)
           AND (consumed_at IS NULL) = (agent_certificate_pem IS NULL)
           AND (consumed_at IS NULL) = (issuer_pem IS NULL)
           AND (consumed_at IS NULL) = (server_root_pem IS NULL)),
    DROP CONSTRAINT agent_enrollments_keys_whole,
    ALTER COLUMN supervisor_key_sha256 SET NOT NULL,
    ALTER COLUMN agent_key_sha256 SET NOT NULL;
