-- The policy service's feed of cowork's entities (ADR-0140): a set of
-- dirty entities that triggers write and a delivery loop reads. It
-- holds no document; policy_entity_state reads what an entity is when
-- it is delivered.
--
-- It alters no stored row. The outbox starts empty and the feed off,
-- so a database that never runs `cowork policy-feed start` writes no
-- mark and takes no lock on the outbox.

CREATE TABLE policy_feed (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled   boolean NOT NULL DEFAULT false
);

INSERT INTO policy_feed (singleton) VALUES (true);

-- The one sequence a delivery takes its changes' sequence from: it
-- rises with every send, which is what the policy service needs of a
-- number that rises with each change to an entity.
CREATE SEQUENCE policy_feed_sequence;

-- One row per entity, so the table is bounded by the entities written
-- since the feed started. entity_type is the GRN type cowork's service
-- gives the entity, and org the organization it is in. A write raises
-- the generation: delivery deletes a mark only at the generation it
-- read, so a write landing mid-pass survives it.
CREATE TABLE policy_outbox (
    entity_type text   NOT NULL CHECK (entity_type IN ('channel', 'page', 'task', 'artifact')),
    org         text   NOT NULL,
    entity_id   text   NOT NULL,
    generation  bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    PRIMARY KEY (org, entity_type, entity_id)
);

CREATE FUNCTION policy_outbox_mark(p_type text, p_org text, p_id text) RETURNS void
LANGUAGE sql VOLATILE AS $$
    INSERT INTO policy_outbox (entity_type, org, entity_id) VALUES (p_type, p_org, p_id)
    ON CONFLICT (org, entity_type, entity_id)
    DO UPDATE SET generation = policy_outbox.generation + 1
$$;

-- The trigger of the four tables below, which hold a Channel, a Page,
-- a Task and an Artifact. It fires on every write: a page's move or a
-- task's retitle reaches no fact the service holds, but marking is
-- cheap and delivery sends only what the tables say. No store updates
-- a key, so an update marks the row's own entity. A cascade fires it
-- row by row, so a deleted page marks the pages its foreign key took.
CREATE FUNCTION policy_outbox_track() RETURNS trigger
LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    v_row jsonb;
BEGIN
    IF NOT (SELECT enabled FROM policy_feed) THEN
        RETURN NULL;
    END IF;
    v_row := CASE TG_OP WHEN 'DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
    PERFORM policy_outbox_mark(
        CASE TG_TABLE_NAME
            WHEN 'channels' THEN 'channel'
            WHEN 'pages' THEN 'page'
            WHEN 'tasks' THEN 'task'
            WHEN 'artifacts' THEN 'artifact'
        END,
        v_row->>'org', v_row->>'id');
    RETURN NULL;
END
$$;

CREATE TRIGGER policy_outbox_channels AFTER INSERT OR UPDATE OR DELETE ON channels
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_pages AFTER INSERT OR UPDATE OR DELETE ON pages
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_tasks AFTER INSERT OR UPDATE OR DELETE ON tasks
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();
CREATE TRIGGER policy_outbox_artifacts AFTER INSERT OR UPDATE OR DELETE ON artifacts
    FOR EACH ROW EXECUTE FUNCTION policy_outbox_track();

-- policy_entity_state is the one surface a delivery reads an entity's
-- state through. It answers no row for an entity the tables no longer
-- hold, which is a delete, and otherwise one row: the entity's
-- organization, which is all cowork's tables know of where it sits.
-- STABLE, so every row it reads comes from one snapshot.
CREATE FUNCTION policy_entity_state(p_type text, p_org text, p_id text)
RETURNS TABLE (org text)
LANGUAGE sql STABLE AS $$
    SELECT org FROM channels WHERE p_type = 'channel' AND org = p_org AND id = p_id
    UNION ALL
    SELECT org FROM pages WHERE p_type = 'page' AND org = p_org AND id = p_id
    UNION ALL
    SELECT org FROM tasks WHERE p_type = 'task' AND org = p_org AND id = p_id
    UNION ALL
    SELECT org FROM artifacts WHERE p_type = 'artifact' AND org = p_org AND id = p_id
$$;
