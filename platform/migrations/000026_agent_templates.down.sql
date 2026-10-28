DROP TABLE IF EXISTS agent_templates;

ALTER TABLE agent_definitions
    DROP COLUMN IF EXISTS name;
