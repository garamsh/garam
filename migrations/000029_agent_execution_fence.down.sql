DROP FUNCTION IF EXISTS agent_execution_admit(text, bytea, text, timestamptz, timestamptz);
DROP FUNCTION IF EXISTS agent_credential_current(text, text, timestamptz, timestamptz);
DROP TABLE IF EXISTS agent_recoveries;
DROP TABLE IF EXISTS agent_credentials;
ALTER TABLE IF EXISTS agent_executions
    DROP CONSTRAINT IF EXISTS agent_executions_activation_fk,
    DROP CONSTRAINT IF EXISTS agent_executions_latest_activation_fk;
DROP TABLE IF EXISTS agent_activations;
DROP TABLE IF EXISTS agent_executions;
