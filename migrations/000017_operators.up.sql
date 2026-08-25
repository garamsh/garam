-- An organization registers an operator, and this table is the record
-- (ADR-0044). Before it, an operator existed only as a GRN somebody
-- composed by hand for `garam issue-certificate` (ADR-0032), so a
-- definition could name one that was never going to exist and nothing
-- reported it.
--
-- A row grants the operator nothing. It is not a credential, not an
-- enrolment, and not a revocation point: an operator still authenticates
-- with the certificate it holds (ADR-0015), and a row that is absent
-- stops no certificate that was already issued.
CREATE TABLE operators (
    grn           text PRIMARY KEY,
    org_grn       text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    registered_by text NOT NULL,
    created_at    timestamptz NOT NULL,

    -- The GRN must name an operator of the org the row belongs to, as
    -- `agents` requires of its own (000007). internal/operator refuses
    -- both halves too; this is the copy no call path routes around.
    CONSTRAINT operators_grn_ownership
        CHECK (split_part(grn, ':', 4) = 'operator'
           AND split_part(grn, ':', 2) = split_part(org_grn, ':', 5))
);

CREATE INDEX operators_org_idx ON operators (org_grn);
