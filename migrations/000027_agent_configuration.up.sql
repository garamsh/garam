-- An agent's desired configuration is revised, and what its operator
-- acknowledges of it is recorded apart from its health (ADR-0076).
--
-- A definition's values now change, so each saved set carries a
-- revision. A definition written before this migration holds the only
-- set it ever had, which is its first revision, saved when and by whom
-- the definition was: no time or author is invented for it.
ALTER TABLE agent_definitions
    ADD COLUMN revision   integer,
    ADD COLUMN revised_at timestamptz,
    ADD COLUMN revised_by text;

UPDATE agent_definitions
SET revision = 1, revised_at = created_at, revised_by = defined_by;

ALTER TABLE agent_definitions
    ALTER COLUMN revision SET NOT NULL,
    ALTER COLUMN revised_at SET NOT NULL,
    ALTER COLUMN revised_by SET NOT NULL,
    ADD CONSTRAINT agent_definitions_revision_positive
        CHECK (revision > 0);

-- `health_reported_at` is when a provisioning or runtime report last
-- landed. `updated_at` is also written when the agent is created, so it
-- cannot say that; a row written before this migration has no report
-- time garam can name and stays NULL until the next report.
--
-- The configuration columns are the operator's last accepted
-- acknowledgement: the assignment epoch it was made at, the revision it
-- names, whether it was applied or rejected, and when garam received
-- it. The epoch is kept because an acknowledgement is the claim of the
-- operator holding the agent at that epoch, and after a reassignment it
-- describes nothing current. No current operator reports one, so every
-- existing row is NULL: unknown, not applied.
ALTER TABLE agents
    ADD COLUMN health_reported_at         timestamptz,
    ADD COLUMN configuration_epoch        integer,
    ADD COLUMN configuration_revision     integer,
    ADD COLUMN configuration_outcome      text,
    ADD COLUMN configuration_reason       text,
    ADD COLUMN configuration_ignored_keys text[],
    ADD COLUMN configuration_reported_at  timestamptz,
    -- internal/agent refuses each of these too; this is the copy no
    -- call path routes around. The acknowledgement is whole or absent,
    -- a rejection carries a reason and no ignored keys, and an
    -- application carries the keys it ignored, possibly none, and no
    -- reason.
    ADD CONSTRAINT agents_configuration_whole CHECK (
        (configuration_epoch IS NULL
            AND configuration_revision IS NULL
            AND configuration_outcome IS NULL
            AND configuration_reason IS NULL
            AND configuration_ignored_keys IS NULL
            AND configuration_reported_at IS NULL)
        OR (configuration_epoch > 0
            AND configuration_revision > 0
            AND configuration_reported_at IS NOT NULL
            AND ((configuration_outcome = 'applied'
                    AND configuration_reason IS NULL
                    AND configuration_ignored_keys IS NOT NULL
                    AND cardinality(configuration_ignored_keys) <= 32)
                OR (configuration_outcome = 'rejected'
                    AND configuration_ignored_keys IS NULL
                    AND btrim(configuration_reason) <> ''
                    AND char_length(configuration_reason) <= 500))));
