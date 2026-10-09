-- name: CreateOperator :exec
INSERT INTO operators (grn, org_grn, registered_by, created_at, name, description)
VALUES ($1, $2, $3, $4, $5, $6);

-- An absent argument keeps the column it would have written, so a
-- caller changing one of the two leaves the other as it stands. The
-- statement names no other column: the GRN, and the certificate and
-- token rows keyed on it, are what an operator authenticates and
-- enrols by, and this write moves none of them (ADR-0074).
-- name: UpdateOperatorDetails :execrows
UPDATE operators
SET name        = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description)
WHERE grn = sqlc.arg('grn');

-- Writes only the control endpoint, which the details update above
-- never names: changing where operation authority is sent is the
-- delegating act, not renaming (#1170).
-- name: SetOperatorControlEndpoint :execrows
UPDATE operators SET control_endpoint = sqlc.narg('control_endpoint')
WHERE grn = sqlc.arg('grn');

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
--
-- The upsert is what makes a re-mint leave one live token (ADR-0067),
-- and the unique key it arbitrates on (000024) is what makes two
-- unrepresentable, so this holds for whatever mints next as well.
-- Registering never reaches the conflict: its operator row is created
-- in the same transaction, so nothing has minted for that GRN yet.
--
-- The sweep excludes the operator being minted for, so the row it
-- would delete and the row the upsert lands on are never the same one.
-- PostgreSQL 17 documentation §7.8.4 is why: sub-statements in WITH
-- "cannot 'see' one another's effects on the target tables", and
-- "trying to update the same row twice in a single statement is not
-- supported. Only one of the modifications takes place, but it is not
-- easy (and sometimes not possible) to reliably predict which one."
-- Should the delete be the one that takes, the mint returns a token
-- with no row behind it and the caller is answered a secret that
-- enrols nothing. This predicate removes the overlap rather than
-- resting on which way the server resolves it.
-- name: MintOperatorEnrollment :exec
WITH pruned AS (
    DELETE FROM operator_enrollment_tokens
    WHERE expires_at <= now() AND operator_grn <> sqlc.arg('operator_grn')
)
INSERT INTO operator_enrollment_tokens (token_hash, operator_grn, minted_by, minted_at, expires_at)
VALUES (sqlc.arg('token_hash'), sqlc.arg('operator_grn'), sqlc.arg('minted_by'),
        sqlc.arg('minted_at'), sqlc.arg('expires_at'))
ON CONFLICT (operator_grn) DO UPDATE
    SET token_hash = EXCLUDED.token_hash,
        minted_by  = EXCLUDED.minted_by,
        minted_at  = EXCLUDED.minted_at,
        expires_at = EXCLUDED.expires_at;

-- The delete is what makes a token single-use: the row is gone when the
-- statement returns, so two callers presenting one token leave one
-- certificate and the loser sees a token that is not there. Expiry is
-- the same answer for the same reason — a caller that spends nothing
-- learns nothing about which of the two happened.
-- name: SpendOperatorEnrollment :one
DELETE FROM operator_enrollment_tokens
WHERE token_hash = $1 AND expires_at > now()
RETURNING operator_grn;

-- Deleting the record is what stops the operator authenticating
-- (ADR-0070). A definition naming the operator refuses it, through the
-- foreign key 000018 declared NO ACTION, which is the copy no call
-- path routes around.
-- name: DeleteOperator :execrows
DELETE FROM operators WHERE grn = $1;

-- The record of the certificate garam last issued goes with the
-- registration. The unspent token has a cascade to go by (000023) and
-- this row has none (000019), so the delete removes it and the two
-- statements are one transaction. Left standing it would outlive the
-- registration exactly as the row ADR-0045's errata found did, and the
-- next registration of the same identifier would read `enrolled`
-- against it and be refused the token it needs.
-- name: DeleteOperatorCertificate :exec
DELETE FROM operator_certificates WHERE operator_grn = $1;

-- name: CountOperatorsByOrg :one
-- The operators the organization has registered, which deleting the
-- organization removes with it, along with any enrollment token that
-- has not been spent. A certificate already issued is not counted:
-- `operator_certificates` carries no foreign key and keeps its row
-- (ADR-0045 errata).
SELECT count(*) FROM operators WHERE org_grn = $1;

-- Re-issuing is these two in one transaction: the delete takes the
-- previous credential and, by 000038's foreign key, every token minted
-- under it, and the insert writes the successor. An operator the
-- organization holds no record of fails the insert's foreign key.
-- name: DeleteOperatorCredential :exec
DELETE FROM operator_credentials WHERE operator_grn = $1;

-- name: InsertOperatorCredential :exec
INSERT INTO operator_credentials (operator_grn, credential_hash, issued_by, issued_at)
VALUES (sqlc.arg('operator_grn'), sqlc.arg('credential_hash'), sqlc.arg('issued_by'), sqlc.arg('issued_at'));

-- Which operator a presented credential belongs to, read beside the
-- registration it rests on.
-- name: GetOperatorCredentialHolder :one
SELECT c.id, c.operator_grn
FROM operator_credentials c
JOIN operators o ON o.grn = c.operator_grn
WHERE c.credential_hash = $1;

-- An exchange sweeps the operator's expired tokens in the statement
-- that records the new one, so the table stays proportional to what is
-- live without a job of its own. The two never touch one row: the
-- sweep deletes what has expired and the insert writes what has not.
-- A credential re-issued between reading its holder and this insert
-- fails the foreign key, and nothing is recorded.
-- name: RecordOperatorToken :exec
WITH swept AS (
    DELETE FROM operator_tokens
    WHERE operator_grn = sqlc.arg('operator_grn') AND expires_at <= now()
)
INSERT INTO operator_tokens (token_hash, credential_id, operator_grn, audience_grn, issued_at, expires_at)
VALUES (sqlc.arg('token_hash'), sqlc.arg('credential_id'), sqlc.arg('operator_grn'),
        sqlc.narg('audience_grn'), sqlc.arg('issued_at'), sqlc.arg('expires_at'));

-- The read every request presenting a token takes (ADR-0100): the
-- token's row joined to the registration it rests on, so a deleted
-- operator or a re-issued credential refuses it from the next request.
-- name: ResolveOperatorToken :one
SELECT t.operator_grn, t.audience_grn, t.expires_at
FROM operator_tokens t
JOIN operators o ON o.grn = t.operator_grn
WHERE t.token_hash = $1 AND t.expires_at > now();
