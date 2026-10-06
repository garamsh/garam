ALTER TABLE agent_recoveries
    DROP CONSTRAINT IF EXISTS agent_recoveries_chain,
    DROP COLUMN IF EXISTS server_root_pem,
    DROP COLUMN IF EXISTS issuer_pem;
