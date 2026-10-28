-- Restores the schema the certificate path wrote, empty: the rows
-- 000059 dropped come back only from a backup. The fence reads the
-- certificate serials of an agent's lineage again, of which there are
-- none.
CREATE TABLE operator_certificates (
    operator_grn text PRIMARY KEY,
    serial       text NOT NULL,
    issued_at    timestamptz NOT NULL,

    CONSTRAINT operator_certificates_operator_service_type
        CHECK (split_part(operator_grn, ':', 4) = 'operator')
);

CREATE TABLE agent_initial_certificates (
    agent_grn       text PRIMARY KEY REFERENCES agent_managed_creations (agent_grn) ON DELETE CASCADE,
    request_id      text NOT NULL CHECK (request_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    operation_ref   text NOT NULL,
    epoch           integer NOT NULL CHECK (epoch > 0),
    request_sha256  bytea NOT NULL CHECK (octet_length(request_sha256) = 32),
    lineage         uuid NOT NULL,
    certificate_pem text NOT NULL,
    issuer_pem      text NOT NULL,
    server_root_pem text NOT NULL,
    not_after       timestamptz NOT NULL,
    created_at      timestamptz NOT NULL
);

CREATE TABLE agent_supervisor_renewals (
    supervisor_grn     text NOT NULL REFERENCES agent_supervisors (grn) ON DELETE CASCADE,
    predecessor_serial text NOT NULL CHECK (predecessor_serial ~ '^[0-9]{1,40}$'),
    request_id         text NOT NULL CHECK (request_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    request_sha256     bytea NOT NULL CHECK (octet_length(request_sha256) = 32),
    serial             text NOT NULL CHECK (serial ~ '^[0-9]{1,40}$'),
    certificate_pem    text NOT NULL,
    issuer_pem         text NOT NULL,
    server_root_pem    text NOT NULL,
    not_after          timestamptz NOT NULL,
    created_at         timestamptz NOT NULL,
    PRIMARY KEY (supervisor_grn, predecessor_serial)
);

CREATE TABLE agent_credentials (
    agent_grn text NOT NULL REFERENCES agent_executions (agent_grn) ON DELETE CASCADE,
    serial    text NOT NULL CHECK (serial ~ '^[0-9]{1,40}$'),
    lineage   uuid NOT NULL,
    not_after timestamptz NOT NULL,
    PRIMARY KEY (agent_grn, serial)
);

CREATE INDEX agent_credentials_lineage_idx ON agent_credentials (agent_grn, lineage);

CREATE OR REPLACE FUNCTION agent_credential_current(
    p_agent_grn text, p_serial text, p_not_before timestamptz, p_not_after timestamptz
) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT p_not_before <= clock_timestamp()
       AND clock_timestamp() < p_not_after
       AND EXISTS (
           SELECT 1 FROM agent_executions e
           WHERE e.agent_grn = p_agent_grn
             AND CASE WHEN left(p_serial, 1) = 't' THEN EXISTS (
                     SELECT 1 FROM agent_tokens t
                     JOIN agent_assignments a
                       ON a.agent_grn = t.agent_grn
                      AND a.assignee_grn = t.assignee_grn
                      AND a.epoch = t.epoch
                     WHERE t.agent_grn = e.agent_grn
                       AND t.serial = p_serial
                       AND t.lineage IS NOT DISTINCT FROM e.lineage
                       AND t.expires_at > clock_timestamp())
                 ELSE (e.lineage IS NULL OR EXISTS (
                     SELECT 1 FROM agent_credentials c
                     WHERE c.agent_grn = e.agent_grn
                       AND c.serial = p_serial
                       AND c.lineage = e.lineage))
                 END)
$$;


ALTER TABLE agent_recoveries
    ADD COLUMN certificate_pem text,
    ADD COLUMN issuer_pem text,
    ADD COLUMN server_root_pem text,
    ADD CONSTRAINT agent_recoveries_chain
        CHECK ((certificate_pem IS NULL AND issuer_pem IS NULL AND server_root_pem IS NULL)
            OR (certificate_pem IS NOT NULL AND issuer_pem IS NOT NULL AND server_root_pem IS NOT NULL)) NOT VALID;

ALTER TABLE agent_supervisors
    ADD COLUMN serial text CHECK (serial ~ '^[0-9]{1,40}$'),
    ADD COLUMN not_after timestamptz,
    ADD CONSTRAINT agent_supervisors_certificate_whole CHECK ((serial IS NULL) = (not_after IS NULL));

ALTER TABLE agent_enrollments
    DROP CONSTRAINT agent_enrollments_consumption_is_whole,
    ADD COLUMN supervisor_key_sha256 bytea CHECK (octet_length(supervisor_key_sha256) = 32),
    ADD COLUMN agent_key_sha256 bytea CHECK (octet_length(agent_key_sha256) = 32),
    ADD COLUMN supervisor_certificate_pem text,
    ADD COLUMN agent_certificate_pem text,
    ADD COLUMN issuer_pem text,
    ADD COLUMN server_root_pem text,
    ADD CONSTRAINT agent_enrollments_keys_whole
        CHECK ((supervisor_key_sha256 IS NULL) = (agent_key_sha256 IS NULL)),
    ADD CONSTRAINT agent_enrollments_direct_is_keyless
        CHECK (placement = 'standalone' OR supervisor_key_sha256 IS NULL),
    ADD CONSTRAINT agent_enrollments_consumption_is_whole
        CHECK ((consumed_at IS NULL) = (request_sha256 IS NULL)
           AND (consumed_at IS NULL) = (epoch IS NULL)
           AND (consumed_at IS NULL) = (lineage IS NULL)
           AND ((consumed_at IS NOT NULL AND supervisor_key_sha256 IS NOT NULL) = (supervisor_certificate_pem IS NOT NULL))
           AND (supervisor_certificate_pem IS NULL) = (agent_certificate_pem IS NULL)
           AND (supervisor_certificate_pem IS NULL) = (issuer_pem IS NULL)
           AND (supervisor_certificate_pem IS NULL) = (server_root_pem IS NULL));
