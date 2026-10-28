-- garam's own agent templates go (ADR-0117). They were retired
-- read-only (ADR-0088) and the control service's templates replaced
-- them; a deployment still holding one fails here, naming the table,
-- rather than losing it in the drop.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent_templates) THEN
        RAISE EXCEPTION 'agent_templates holds a row: garam keeps no agent template (ADR-0117)';
    END IF;
END
$$;

DROP TABLE agent_templates;
DROP FUNCTION agent_templates_retired();
