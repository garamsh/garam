-- An action is `<service>:<type>-<verb>` (ADR-0143 items 3 and 4): a
-- delegated operation written `<type>:<verb>` is `iam:<type>-<verb>`.
-- The checks that list the names restate them in that form.
--
-- What this does to stored rows: each operation a delegation grant
-- carries (operator_delegations.operations) and each operation an
-- authority records (operation_authorities.operation) is rewritten in
-- place to `iam:` followed by the name with its colon made a hyphen, in
-- the order it was held, so a grant or an authority minted before this
-- migration still names what it covers and a reference to it still
-- resolves. A name already carrying `iam:` is left alone. No stored
-- policy document is converted, and no other table is touched. The
-- checks keep their names and return NOT VALID, as 000058's are.
ALTER TABLE operator_delegations
    DROP CONSTRAINT operator_delegations_operations;

ALTER TABLE operation_authorities
    DROP CONSTRAINT operation_authorities_operation,
    DROP CONSTRAINT operation_authorities_assignment,
    DROP CONSTRAINT operation_authorities_request_target,
    DROP CONSTRAINT operation_authorities_destination;

UPDATE operator_delegations
SET operations = ARRAY(
    SELECT 'iam:' || replace(o.name, ':', '-')
    FROM unnest(operations) WITH ORDINALITY AS o (name, position)
    ORDER BY o.position)
WHERE EXISTS (SELECT 1 FROM unnest(operations) AS o (name) WHERE o.name NOT LIKE 'iam:%');

UPDATE operation_authorities
SET operation = 'iam:' || replace(operation, ':', '-')
WHERE operation NOT LIKE 'iam:%';

ALTER TABLE operator_delegations
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 10
           AND operations <@ ARRAY['iam:agent-create', 'iam:agent-configure', 'iam:agent-activate',
                                   'iam:agent-recover', 'iam:agent-template-read',
                                   'iam:agent-template-publish', 'iam:execution-profile-read',
                                   'iam:agent-execution-read', 'iam:operator-read',
                                   'iam:agent-replace']) NOT VALID;

ALTER TABLE operation_authorities
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('iam:agent-create', 'iam:agent-configure', 'iam:agent-recover',
                             'iam:agent-template-read', 'iam:agent-template-publish',
                             'iam:execution-profile-read', 'iam:agent-execution-read',
                             'iam:operator-read', 'iam:agent-replace')) NOT VALID,
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation IN ('iam:agent-configure', 'iam:agent-recover', 'iam:agent-execution-read',
                              'iam:agent-replace'))
                = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)) NOT VALID,
    ADD CONSTRAINT operation_authorities_request_target
        CHECK ((operation IN ('iam:agent-template-read', 'iam:agent-template-publish',
                              'iam:execution-profile-read', 'iam:agent-execution-read',
                              'iam:operator-read')) = (request_target IS NOT NULL)
           AND (request_target IS NULL OR char_length(request_target) BETWEEN 1 AND 2048)) NOT VALID,
    ADD CONSTRAINT operation_authorities_destination
        CHECK ((operation = 'iam:agent-replace') = (destination_grn IS NOT NULL)) NOT VALID;
