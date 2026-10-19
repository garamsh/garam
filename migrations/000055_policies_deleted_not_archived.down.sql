ALTER TABLE policies
    DROP CONSTRAINT policies_status_check,
    ADD CONSTRAINT policies_status_check CHECK (status IN ('draft', 'active', 'archived'));
