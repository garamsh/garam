-- An administrator registers where a hosted operator's control service
-- answers the console, and a member hands it reads and template
-- publication one request target at a time (#1170).
--
-- The control endpoint is the one URL the console sends an operation
-- authority to. It sits on the operator's row, beside the details an
-- ordinary update writes, and only the endpoint's own write moves it:
-- changing where authority is sent is the delegating act, not renaming.
-- An operation already minted names its audience by GRN, so moving the
-- endpoint moves no audience.
ALTER TABLE operators
    ADD COLUMN control_endpoint text,
    ADD CONSTRAINT operators_control_endpoint
        CHECK (control_endpoint IS NULL
            OR (char_length(control_endpoint) <= 2048 AND control_endpoint LIKE 'https://%'));

-- The reads and the publication are delegated as create is: the three
-- organization operations name no controller, and agent:execution-read
-- acts on an existing agent, so its authority records where the agent
-- runs when it is minted, as configure's does.
ALTER TABLE operator_delegations
    DROP CONSTRAINT operator_delegations_operations,
    ADD CONSTRAINT operator_delegations_operations
        CHECK (cardinality(operations) BETWEEN 1 AND 8
           AND operations <@ ARRAY['agent:create', 'agent:configure', 'agent:activate', 'agent:recover',
                                   'agent-template:read', 'agent-template:publish',
                                   'execution-profile:read', 'agent:execution-read']);

-- An empty body's digest cannot tell one read from another, so the four
-- new operations also bind the exact request target the Operator
-- receives: its escaped path and raw query, with no origin. The three
-- earlier operations bind none, which keeps their rows as they were.
--
-- The request-target check is NOT VALID: every new write is held to
-- it, and a read authority that outlived a rollback of this migration,
-- whose target the rollback dropped, is kept rather than refused.
ALTER TABLE operation_authorities
    ADD COLUMN request_target text,
    DROP CONSTRAINT operation_authorities_operation,
    DROP CONSTRAINT operation_authorities_assignment,
    ADD CONSTRAINT operation_authorities_operation
        CHECK (operation IN ('agent:create', 'agent:configure', 'agent:recover',
                             'agent-template:read', 'agent-template:publish',
                             'execution-profile:read', 'agent:execution-read')),
    ADD CONSTRAINT operation_authorities_assignment
        CHECK ((operation IN ('agent:configure', 'agent:recover', 'agent:execution-read')) = (assignee_grn IS NOT NULL)
           AND (assignee_grn IS NULL) = (assignment_epoch IS NULL)),
    ADD CONSTRAINT operation_authorities_request_target
        CHECK ((operation IN ('agent-template:read', 'agent-template:publish',
                              'execution-profile:read', 'agent:execution-read')) = (request_target IS NOT NULL)
           AND (request_target IS NULL OR char_length(request_target) BETWEEN 1 AND 2048)) NOT VALID;
