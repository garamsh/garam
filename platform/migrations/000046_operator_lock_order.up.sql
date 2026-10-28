-- Deleting an operator deletes the tokens naming it as audience
-- itself, in token_hash order, rather than leaving them to 000041's
-- cascade, which scans the table for them (#1424).
CREATE INDEX operator_tokens_audience_grn_idx ON operator_tokens (audience_grn);

-- Deleting an organization holds its operators' registrations in GRN
-- byte order before its cascade deletes them in whatever order the
-- scan meets them. An exchange holds its client's and its audience's
-- in the same order, so neither waits on one while holding the other
-- the other needs (#1424).
CREATE FUNCTION orgs_hold_operators() RETURNS trigger AS $$
BEGIN
    PERFORM 1 FROM operators WHERE org_grn = OLD.grn ORDER BY grn COLLATE "C" FOR UPDATE;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER orgs_hold_operators
    BEFORE DELETE ON orgs
    FOR EACH ROW EXECUTE FUNCTION orgs_hold_operators();
