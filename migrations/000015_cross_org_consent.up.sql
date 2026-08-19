-- The consent relation a cross-organization message needs (ADR-0035):
-- one row per pair of organizations, holding each side's half as the
-- moment that side last granted. The relation stands while both halves
-- do; either organization withdraws its own half at any time, and a row
-- neither half stands in is deleted, so the table holds only live
-- consent. A message crosses between the two organizations while the
-- relation stands and is refused back to its sender when it does not.
--
-- The pair is stored ordered, so (acme, globex) and (globex, acme) are
-- one row and no second index has to cover the other direction. Both
-- columns reference the organizations themselves: a consent row cannot
-- name an organization that does not exist, and an organization that is
-- deleted takes its halves with it.
CREATE TABLE message_consents (
    org_a          text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    org_b          text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    a_consented_at timestamptz,
    b_consented_at timestamptz,
    PRIMARY KEY (org_a, org_b),
    CONSTRAINT message_consents_ordered CHECK (org_a < org_b),
    CONSTRAINT message_consents_a_half_stands CHECK (a_consented_at IS NOT NULL OR b_consented_at IS NOT NULL)
);

-- Listing one organization's relations reads both sides of the pair;
-- the primary key covers org_a, this covers org_b.
CREATE INDEX message_consents_org_b_idx ON message_consents (org_b);
