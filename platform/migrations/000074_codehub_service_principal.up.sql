-- codehub's service principal in every organization that exists now,
-- grn:<org>/iam/service/codehub, the second of the closed list of
-- first-party services (ADR-0129 item 1, ADR-0143 item 7). An
-- organization created after this is given it by CreateOrg.
--
-- What this does to stored rows: it adds a principal named codehub to
-- each organization that has none, and changes no existing row. An
-- organization whose GRN is still the five-segment form 000069 left
-- unrewritten gets none, for the reason 000073 gave.
INSERT INTO service_principals (grn, org_grn)
SELECT 'grn:' || grn_id(o.grn) || '/iam/service/codehub', o.grn
FROM orgs AS o
WHERE grn_id(o.grn) <> ''
ON CONFLICT (grn) DO NOTHING;
