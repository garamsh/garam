-- garam accepts no new desired runtime value (ADR-0090). An agent is
-- created through its operator's control service (ADR-0077), and the
-- definitions here stay as written: read, claimed by the operator each
-- names and cut over (ADR-0086), and rewritten by nothing.
--
-- The guard is the schema's rather than the routes', so it holds for
-- every writer, a release still serving the define route included.
-- Creating the trigger waits for every write already holding the
-- table, which commits first; a write that reaches the table after it
-- is refused. Renaming changes the name alone and is admitted
-- (ADR-0075). A definition still goes with its organization: a cascade
-- runs after the organization's row is gone, which is what tells it
-- from a direct delete, as 000034 tells a template's.
CREATE FUNCTION agent_definitions_closed() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM orgs WHERE grn = OLD.org_grn) THEN
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE'
       AND (NEW.agent_grn, NEW.org_grn, NEW.operator_grn, NEW.agent_values, NEW.defined_by, NEW.created_at)
           IS NOT DISTINCT FROM
           (OLD.agent_grn, OLD.org_grn, OLD.operator_grn, OLD.agent_values, OLD.defined_by, OLD.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'agent_definitions accepts no desired value';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER agent_definitions_closed
    BEFORE INSERT OR UPDATE OR DELETE ON agent_definitions
    FOR EACH ROW EXECUTE FUNCTION agent_definitions_closed();
