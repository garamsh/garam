ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_configuration_whole,
    DROP COLUMN IF EXISTS configuration_reported_at,
    DROP COLUMN IF EXISTS configuration_ignored_keys,
    DROP COLUMN IF EXISTS configuration_reason,
    DROP COLUMN IF EXISTS configuration_outcome,
    DROP COLUMN IF EXISTS configuration_revision,
    DROP COLUMN IF EXISTS configuration_epoch,
    DROP COLUMN IF EXISTS health_reported_at;

ALTER TABLE agent_definitions
    DROP CONSTRAINT IF EXISTS agent_definitions_revision_positive,
    DROP COLUMN IF EXISTS revised_by,
    DROP COLUMN IF EXISTS revised_at,
    DROP COLUMN IF EXISTS revision;
