DROP TRIGGER IF EXISTS orgs_hold_before_cascade ON orgs;
DROP FUNCTION IF EXISTS orgs_hold_before_cascade();

CREATE FUNCTION orgs_hold_operators() RETURNS trigger AS $$
BEGIN
    PERFORM 1 FROM operators WHERE org_grn = OLD.grn ORDER BY grn COLLATE "C" FOR UPDATE;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER orgs_hold_operators
    BEFORE DELETE ON orgs
    FOR EACH ROW EXECUTE FUNCTION orgs_hold_operators();

DROP TRIGGER IF EXISTS operators_hold_agent_tokens ON operators;
DROP FUNCTION IF EXISTS operators_hold_agent_tokens();
DROP INDEX IF EXISTS agent_tokens_audience_grn_idx;
