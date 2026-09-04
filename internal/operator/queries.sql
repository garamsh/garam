-- name: CreateOperator :exec
INSERT INTO operators (grn, org_grn, registered_by, created_at)
VALUES ($1, $2, $3, $4);

-- Where an operator stands in obtaining a certificate is derived from
-- the two tables beside the record and stored in neither: a row in
-- `operator_certificates` says garam issued one, and a token still
-- inside its window says one was asked for and not yet obtained.
--
-- The token join carries `expires_at > now()`, so a token nobody spent
-- answers as no token at all. Without that predicate an expired token
-- and a live one are one answer, which is the whole of what the state
-- is read for.
-- name: GetOperator :one
SELECT sqlc.embed(o),
       c.issued_at AS certificate_issued_at,
       (t.token_hash IS NOT NULL)::boolean AS live_token
FROM operators o
LEFT JOIN operator_certificates c ON c.operator_grn = o.grn
LEFT JOIN operator_enrollment_tokens t
       ON t.operator_grn = o.grn AND t.expires_at > now()
WHERE o.grn = $1;

-- name: ListOperatorsByOrg :many
SELECT sqlc.embed(o),
       c.issued_at AS certificate_issued_at,
       (t.token_hash IS NOT NULL)::boolean AS live_token
FROM operators o
LEFT JOIN operator_certificates c ON c.operator_grn = o.grn
LEFT JOIN operator_enrollment_tokens t
       ON t.operator_grn = o.grn AND t.expires_at > now()
WHERE o.org_grn = $1
ORDER BY o.grn;

-- name: AdoptOperatorCertificate :exec
INSERT INTO operator_certificates (operator_grn, serial, issued_at)
VALUES (sqlc.arg('operator_grn'), sqlc.arg('serial'), sqlc.arg('issued_at'))
ON CONFLICT (operator_grn) DO UPDATE
    SET serial = EXCLUDED.serial, issued_at = EXCLUDED.issued_at;

-- Moves the record only where it still holds the serial the caller
-- presented, so two holders of one identity cannot both carry the
-- lineage on. An operator with no row yet is adopted by the insert.
-- name: AdvanceOperatorCertificate :execrows
INSERT INTO operator_certificates (operator_grn, serial, issued_at)
VALUES (sqlc.arg('operator_grn'), sqlc.arg('serial'), sqlc.arg('issued_at'))
ON CONFLICT (operator_grn) DO UPDATE
    SET serial = EXCLUDED.serial, issued_at = EXCLUDED.issued_at
    WHERE operator_certificates.serial = sqlc.arg('predecessor');

-- Minting sweeps the tokens that have expired, as creating an
-- invitation sweeps its own table: a row is written in one place, so
-- sweeping there keeps the table proportional to what is live.
-- name: MintOperatorEnrollment :exec
WITH pruned AS (
    DELETE FROM operator_enrollment_tokens WHERE expires_at <= now()
)
INSERT INTO operator_enrollment_tokens (token_hash, operator_grn, minted_by, minted_at, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- The delete is what makes a token single-use: the row is gone when the
-- statement returns, so two callers presenting one token leave one
-- certificate and the loser sees a token that is not there. Expiry is
-- the same answer for the same reason — a caller that spends nothing
-- learns nothing about which of the two happened.
-- name: SpendOperatorEnrollment :one
DELETE FROM operator_enrollment_tokens
WHERE token_hash = $1 AND expires_at > now()
RETURNING operator_grn;

-- name: CountOperatorsByOrg :one
-- The operators the organization has registered, which deleting the
-- organization removes with it, along with any enrollment token that
-- has not been spent. A certificate already issued is not counted:
-- `operator_certificates` carries no foreign key and keeps its row
-- (ADR-0045 errata).
SELECT count(*) FROM operators WHERE org_grn = $1;
