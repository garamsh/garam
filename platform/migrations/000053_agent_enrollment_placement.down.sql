-- An unspent direct approval has no shape below this migration, so it
-- goes. A spent one stays, since its supervisor's registration
-- references it, and reads as a standalone approval from then on.
DELETE FROM agent_enrollments WHERE placement = 'direct' AND consumed_at IS NULL;

ALTER TABLE agent_enrollments
    DROP CONSTRAINT IF EXISTS agent_enrollments_replaced_activation_is_spent,
    DROP CONSTRAINT IF EXISTS agent_enrollments_direct_is_keyless,
    DROP COLUMN IF EXISTS replaced_activation_id,
    DROP COLUMN IF EXISTS placement;
