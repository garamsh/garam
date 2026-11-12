-- Holds a principal's record for the transaction and answers the
-- database's clock, which decides whether its assertion is timely. A
-- record that is gone answers no row. FOR KEY SHARE blocks the
-- organization's deletion from removing it before this commits, and
-- blocks nothing else.
-- name: HoldServicePrincipal :one
SELECT now()::timestamptz AS decided_at
FROM service_principals
WHERE grn = $1
FOR KEY SHARE;

-- Answers whether an organization of this id exists, with the
-- database's clock, for an assertion whose principal has no record. It
-- reads, locks and writes nothing.
-- name: OrganizationHeld :one
SELECT now()::timestamptz AS decided_at,
       EXISTS (SELECT 1 FROM orgs WHERE grn_id(grn) = sqlc.arg('org')::text) AS held;

-- Drops the principal's spent identifiers past their keeping. It is a
-- statement of its own rather than the spend's CTE, which would share
-- the spend's snapshot and still conflict with a row it deletes.
-- name: SweepServiceAssertions :exec
DELETE FROM operator_assertions WHERE operator_grn = $1 AND expires_at <= now();

-- Spends a (principal, jti) in the table the operator exchange spends
-- in. A conflict answers no row: the assertion was spent already, by
-- this replica or another (ADR-0129 item 2).
-- name: SpendServiceAssertion :one
INSERT INTO operator_assertions (operator_grn, jti, expires_at)
VALUES (sqlc.arg('operator_grn'), sqlc.arg('jti'), sqlc.arg('expires_at'))
ON CONFLICT (operator_grn, jti) DO NOTHING
RETURNING jti;
