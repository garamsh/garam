-- An organization's agent templates are retired read-only (ADR-0088).
-- The Operator control service is the one source agents are created
-- from (ADR-0077); the rows here stay as the record of what each
-- template said, and nothing writes one again.
--
-- The guard is the schema's rather than the routes', so it holds for
-- every writer, a release still serving the routes included. Creating
-- the trigger waits for every write already holding the table, which
-- commits first; a write that reaches the table after it is refused.
-- A row still goes with its organization: a cascade runs after the
-- organization's row is gone, which is what tells it from a direct
-- delete, as 000002 tells a policy version's.
CREATE FUNCTION agent_templates_retired() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM orgs WHERE grn = OLD.org_grn) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'agent_templates is retired read-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER agent_templates_retired
    BEFORE INSERT OR UPDATE OR DELETE ON agent_templates
    FOR EACH ROW EXECUTE FUNCTION agent_templates_retired();
