-- A policy is retired by deleting it, with its versions and its
-- attachments, and no longer by archiving it (ADR-0117). The status
-- check stops admitting 'archived', so a deployment still holding an
-- archived policy fails here rather than keeping a state nothing
-- reads.
ALTER TABLE policies
    DROP CONSTRAINT policies_status_check,
    ADD CONSTRAINT policies_status_check CHECK (status IN ('draft', 'active'));
