-- A role link the policy service refuses for good is set aside, as an
-- entity it refuses is (ADR-0140 item 11): the delivery pass stops
-- retrying it and goes on with the rest. Unlike an entity, whose mark
-- is deleted after its delete is sent, the link's mark stays, so the
-- refusal is a row an operator reads and a request waiting at the gate
-- is answered from, whichever replica delivered the pass.
--
-- What this does to stored rows: policy_outbox keeps every row, and
-- the three columns are NULL in each, which is a mark that is pending.
-- Nothing is rewritten.
--
-- A set-aside mark is read by nothing that delivers. A later write of
-- the same entity raises its generation and clears the columns (the
-- redefined policy_outbox_mark below, and `garam policy-feed start`'s
-- backfill), so a membership that changes is delivered again.
--
-- Only policy_outbox is locked, and no writer holds a lock this
-- migration waits for while holding one it needs, so it cannot join a
-- deadlock with a writer of a membership.
ALTER TABLE policy_outbox
    ADD COLUMN set_aside_code   text,
    ADD COLUMN set_aside_reason text,
    ADD COLUMN set_aside_at     timestamptz,
    ADD CONSTRAINT policy_outbox_set_aside_check CHECK (
        (set_aside_code IS NULL) = (set_aside_at IS NULL)
        AND (set_aside_reason IS NULL) = (set_aside_at IS NULL)
        AND (set_aside_at IS NULL OR entity_type = 'Iam::RoleLink'));

CREATE OR REPLACE FUNCTION policy_outbox_mark(p_type text, p_id text) RETURNS void
LANGUAGE sql VOLATILE AS $$
    INSERT INTO policy_outbox (entity_type, entity_id) VALUES (p_type, p_id)
    ON CONFLICT (entity_type, entity_id)
    DO UPDATE SET generation = policy_outbox.generation + 1,
        set_aside_code = NULL, set_aside_reason = NULL, set_aside_at = NULL
$$;
