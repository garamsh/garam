-- The names as 000058 left them: `iam:<type>-<verb>` is `<type>:<verb>`
-- for the four types a delegated operation names, in the rows and in
-- the checks. The rows are rewritten in place and in the order they
-- were held, and every check is restored under the name it had, which
-- 000058's down drops. The checks return NOT VALID.
--
-- The checks are written from the names 000071's up lists, each turned
-- back by the same rewrite as the rows, so the two cannot disagree.
ALTER TABLE operator_delegations
    DROP CONSTRAINT operator_delegations_operations;

ALTER TABLE operation_authorities
    DROP CONSTRAINT operation_authorities_operation,
    DROP CONSTRAINT operation_authorities_assignment,
    DROP CONSTRAINT operation_authorities_request_target,
    DROP CONSTRAINT operation_authorities_destination;

CREATE FUNCTION pg_temp.unqualified(name text) RETURNS text
LANGUAGE sql IMMUTABLE AS $$
    SELECT regexp_replace(name, '^iam:(agent-template|execution-profile|agent|operator)-', '\1:')
$$;

UPDATE operator_delegations
SET operations = ARRAY(
    SELECT pg_temp.unqualified(o.name)
    FROM unnest(operations) WITH ORDINALITY AS o (name, position)
    ORDER BY o.position)
WHERE EXISTS (SELECT 1 FROM unnest(operations) AS o (name) WHERE o.name LIKE 'iam:%');

UPDATE operation_authorities
SET operation = pg_temp.unqualified(operation)
WHERE operation LIKE 'iam:%';

DO $$
DECLARE
    v_all text[] := ARRAY(SELECT pg_temp.unqualified(n) FROM unnest(ARRAY[
        'iam:agent-create', 'iam:agent-configure', 'iam:agent-activate', 'iam:agent-recover',
        'iam:agent-template-read', 'iam:agent-template-publish', 'iam:execution-profile-read',
        'iam:agent-execution-read', 'iam:operator-read', 'iam:agent-replace']) AS n);
    v_minted text[] := array_remove(v_all, pg_temp.unqualified('iam:agent-activate'));
    v_assigned text[] := ARRAY(SELECT pg_temp.unqualified(n) FROM unnest(ARRAY[
        'iam:agent-configure', 'iam:agent-recover', 'iam:agent-execution-read',
        'iam:agent-replace']) AS n);
    v_targeted text[] := ARRAY(SELECT pg_temp.unqualified(n) FROM unnest(ARRAY[
        'iam:agent-template-read', 'iam:agent-template-publish', 'iam:execution-profile-read',
        'iam:agent-execution-read', 'iam:operator-read']) AS n);
BEGIN
    EXECUTE format(
        'ALTER TABLE operator_delegations ADD CONSTRAINT operator_delegations_operations
            CHECK (cardinality(operations) BETWEEN 1 AND 10 AND operations <@ %L::text[]) NOT VALID',
        v_all);
    EXECUTE format(
        'ALTER TABLE operation_authorities
            ADD CONSTRAINT operation_authorities_operation
                CHECK (operation = ANY (%L::text[])) NOT VALID,
            ADD CONSTRAINT operation_authorities_assignment
                CHECK ((operation = ANY (%L::text[])) = (assignee_grn IS NOT NULL)
                   AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)) NOT VALID,
            ADD CONSTRAINT operation_authorities_request_target
                CHECK ((operation = ANY (%L::text[])) = (request_target IS NOT NULL)
                   AND (request_target IS NULL OR char_length(request_target) BETWEEN 1 AND 2048)) NOT VALID,
            ADD CONSTRAINT operation_authorities_destination
                CHECK ((operation = %L) = (destination_grn IS NOT NULL)) NOT VALID',
        v_minted, v_assigned, v_targeted, pg_temp.unqualified('iam:agent-replace'));
END
$$;
