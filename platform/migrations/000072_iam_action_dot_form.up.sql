-- An action is `<service>:<type>.<verb>` (ADR-0145 item 1): a delegated
-- operation 000071 wrote `iam:<type>-<verb>` is `iam:<type>.<verb>`,
-- the type being the one the operation names, so `iam:agent-template-
-- read` is `iam:agent-template.read` and not `iam:agent.template-read`.
-- The checks that list the names restate them in that form.
--
-- What this does to stored rows: each operation a delegation grant
-- carries (operator_delegations.operations) and each operation an
-- authority records (operation_authorities.operation) is rewritten in
-- place by the explicit mapping below, one name to one name, in the
-- order it was held, so a grant or an authority minted before this
-- migration still names what it covers and a reference to it still
-- resolves. A name the mapping does not list is left as it is. No
-- stored policy document is converted, and no other table is touched.
-- The checks keep their names and return NOT VALID, as 000071's do.
ALTER TABLE operator_delegations
    DROP CONSTRAINT operator_delegations_operations;

ALTER TABLE operation_authorities
    DROP CONSTRAINT operation_authorities_operation,
    DROP CONSTRAINT operation_authorities_assignment,
    DROP CONSTRAINT operation_authorities_request_target,
    DROP CONSTRAINT operation_authorities_destination;

CREATE FUNCTION pg_temp.dotted(name text) RETURNS text
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE name
        WHEN 'iam:agent-create' THEN 'iam:agent.create'
        WHEN 'iam:agent-configure' THEN 'iam:agent.configure'
        WHEN 'iam:agent-activate' THEN 'iam:agent.activate'
        WHEN 'iam:agent-recover' THEN 'iam:agent.recover'
        WHEN 'iam:agent-template-read' THEN 'iam:agent-template.read'
        WHEN 'iam:agent-template-publish' THEN 'iam:agent-template.publish'
        WHEN 'iam:execution-profile-read' THEN 'iam:execution-profile.read'
        WHEN 'iam:agent-execution-read' THEN 'iam:agent.execution-read'
        WHEN 'iam:operator-read' THEN 'iam:operator.read'
        WHEN 'iam:agent-replace' THEN 'iam:agent.replace'
        ELSE name
    END
$$;

UPDATE operator_delegations
SET operations = ARRAY(
    SELECT pg_temp.dotted(o.name)
    FROM unnest(operations) WITH ORDINALITY AS o (name, position)
    ORDER BY o.position)
WHERE EXISTS (SELECT 1 FROM unnest(operations) AS o (name) WHERE pg_temp.dotted(o.name) <> o.name);

UPDATE operation_authorities
SET operation = pg_temp.dotted(operation)
WHERE pg_temp.dotted(operation) <> operation;

ALTER TABLE operator_delegations
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 10
           AND operations <@ ARRAY['iam:agent.create', 'iam:agent.configure', 'iam:agent.activate',
                                   'iam:agent.recover', 'iam:agent-template.read',
                                   'iam:agent-template.publish', 'iam:execution-profile.read',
                                   'iam:agent.execution-read', 'iam:operator.read',
                                   'iam:agent.replace']) NOT VALID;

ALTER TABLE operation_authorities
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('iam:agent.create', 'iam:agent.configure', 'iam:agent.recover',
                             'iam:agent-template.read', 'iam:agent-template.publish',
                             'iam:execution-profile.read', 'iam:agent.execution-read',
                             'iam:operator.read', 'iam:agent.replace')) NOT VALID,
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation IN ('iam:agent.configure', 'iam:agent.recover', 'iam:agent.execution-read',
                              'iam:agent.replace'))
                = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)) NOT VALID,
    ADD CONSTRAINT operation_authorities_request_target
        CHECK ((operation IN ('iam:agent-template.read', 'iam:agent-template.publish',
                              'iam:execution-profile.read', 'iam:agent.execution-read',
                              'iam:operator.read')) = (request_target IS NOT NULL)
           AND (request_target IS NULL OR char_length(request_target) BETWEEN 1 AND 2048)) NOT VALID,
    ADD CONSTRAINT operation_authorities_destination
        CHECK ((operation = 'iam:agent.replace') = (destination_grn IS NOT NULL)) NOT VALID;
