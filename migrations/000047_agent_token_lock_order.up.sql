-- Deleting an operator holds the agent tokens naming it as audience in
-- token_hash order before 000041's cascade deletes them, the order a
-- mint's trim takes an agent's tokens in, so neither waits on one
-- while holding another the other needs (#1434). The index is the
-- holding statement's, and the cascade's, way to them.
CREATE INDEX agent_tokens_audience_grn_idx ON agent_tokens (audience_grn);

CREATE FUNCTION operators_hold_agent_tokens() RETURNS trigger AS $$
BEGIN
    PERFORM 1 FROM agent_tokens WHERE audience_grn = OLD.grn ORDER BY token_hash FOR UPDATE;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER operators_hold_agent_tokens
    BEFORE DELETE ON operators
    FOR EACH ROW EXECUTE FUNCTION operators_hold_agent_tokens();

-- Deleting an organization holds its agents' execution rows before its
-- operators' registrations, both in byte order. A mint holds an
-- execution row and then, at its insert's foreign key, the audience's
-- registration; 000046 held the registrations first, the opposite
-- order (#1434).
DROP TRIGGER orgs_hold_operators ON orgs;
DROP FUNCTION orgs_hold_operators();

CREATE FUNCTION orgs_hold_before_cascade() RETURNS trigger AS $$
BEGIN
    PERFORM 1 FROM agent_executions e JOIN agents a ON a.grn = e.agent_grn
        WHERE a.org_grn = OLD.grn ORDER BY e.agent_grn COLLATE "C" FOR UPDATE OF e;
    PERFORM 1 FROM operators WHERE org_grn = OLD.grn ORDER BY grn COLLATE "C" FOR UPDATE;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER orgs_hold_before_cascade
    BEFORE DELETE ON orgs
    FOR EACH ROW EXECUTE FUNCTION orgs_hold_before_cascade();
