CREATE TABLE agent_templates (
    grn          text PRIMARY KEY,
    org_grn      text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    agent_values jsonb NOT NULL,
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,

    CONSTRAINT agent_templates_grn_ownership
        CHECK (split_part(grn, ':', 4) = 'agent-template'
           AND split_part(grn, ':', 2) = split_part(org_grn, ':', 5)),
    CONSTRAINT agent_templates_name_length
        CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT agent_templates_description_length
        CHECK (char_length(description) <= 1000),
    CONSTRAINT agent_templates_values_object
        CHECK (jsonb_typeof(agent_values) = 'object')
);

CREATE INDEX agent_templates_org_idx ON agent_templates (org_grn);

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
