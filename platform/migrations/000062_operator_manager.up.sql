-- An Operator is two identities, its manager and its control service,
-- and each is registered as an operator of its own (ADR-0124). The
-- control service's record names the manager it serves, so a read of
-- the organization's operators answers the pair without inferring it
-- from names or from a delegation, which lapses and may overlap.
--
-- The manager is named by GRN and not by a foreign key. The identity
-- is configured before it is registered, so a control service still
-- names the manager it was configured for after that manager's record
-- is deleted, and deleting a manager is not refused for having one.
-- internal/operator refuses a manager that is not registered when the
-- control service is.
--
-- At most one control service names a manager, a record never names
-- itself, and the manager is an operator of the same organization.
ALTER TABLE operators
    ADD COLUMN manager_grn text,
    ADD CONSTRAINT operators_manager_unique UNIQUE (manager_grn),
    ADD CONSTRAINT operators_manager
        CHECK (manager_grn IS NULL
            OR (manager_grn <> grn
                AND split_part(manager_grn, ':', 4) = 'operator'
                AND split_part(manager_grn, ':', 2) = split_part(grn, ':', 2)));
