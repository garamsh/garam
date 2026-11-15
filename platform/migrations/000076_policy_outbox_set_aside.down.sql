-- The outbox as 000075 left it. A mark that was set aside becomes a
-- pending one: nothing is deleted, and a delivery that does not know
-- the columns reads it as it read every mark, so the refusal that set
-- it aside stops the pass again until the link is repaired or the feed
-- is stopped. That is the state before this migration, not a loss.
CREATE OR REPLACE FUNCTION policy_outbox_mark(p_type text, p_id text) RETURNS void
LANGUAGE sql VOLATILE AS $$
    INSERT INTO policy_outbox (entity_type, entity_id) VALUES (p_type, p_id)
    ON CONFLICT (entity_type, entity_id)
    DO UPDATE SET generation = policy_outbox.generation + 1
$$;

ALTER TABLE policy_outbox
    DROP CONSTRAINT policy_outbox_set_aside_check,
    DROP COLUMN set_aside_code,
    DROP COLUMN set_aside_reason,
    DROP COLUMN set_aside_at;
