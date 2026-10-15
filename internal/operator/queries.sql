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
-- sweeping there keeps the table proportional to what is live. The
-- sweep skips a row another transaction holds, which a later sweep
-- takes, so the one row a mint waits on is its own operator's (#1424).
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
    WHERE token_hash IN (
        SELECT e.token_hash FROM operator_enrollment_tokens e
        WHERE e.expires_at <= now() AND e.operator_grn <> sqlc.arg('operator_grn')
        FOR UPDATE SKIP LOCKED)
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

-- Every token the operator holds or is the audience of, which the
-- deletion's cascades would otherwise take in their own orders. Like
-- every statement that may wait on more than one token, it takes them
-- in token_hash order (#1424).
-- name: DeleteOperatorTokens :exec
WITH doomed AS MATERIALIZED (
    SELECT t.token_hash FROM operator_tokens t
    WHERE t.operator_grn = sqlc.arg('operator_grn') OR t.audience_grn = sqlc.arg('operator_grn')
    ORDER BY t.token_hash
    FOR UPDATE OF t)
DELETE FROM operator_tokens WHERE token_hash IN (SELECT token_hash FROM doomed);

-- name: CountOperatorsByOrg :one
-- The operators the organization has registered, which deleting the
-- organization removes with it, along with any enrollment token that
-- has not been spent. A certificate already issued is not counted:
-- `operator_certificates` carries no foreign key and keeps its row
-- (ADR-0045 errata).
SELECT count(*) FROM operators WHERE org_grn = $1;

-- Re-issuing is these three in one transaction: the first deletes
-- every token minted under the previous credential, in token_hash
-- order, which 000038's cascade would take in its own; the second the
-- credential, and the insert writes the successor. An operator the
-- organization holds no record of fails the insert's foreign key.
-- name: DeleteOperatorCredentialTokens :exec
WITH doomed AS MATERIALIZED (
    SELECT t.token_hash FROM operator_tokens t
    JOIN operator_credentials c ON c.id = t.credential_id
    WHERE c.operator_grn = sqlc.arg('operator_grn')
    ORDER BY t.token_hash
    FOR UPDATE OF t)
DELETE FROM operator_tokens WHERE token_hash IN (SELECT token_hash FROM doomed);

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

-- name: RecordOperatorToken :exec
INSERT INTO operator_tokens (token_hash, credential_id, operator_grn, audience_grn, issued_at, expires_at)
VALUES (sqlc.arg('token_hash'), sqlc.arg('credential_id')::bigint, sqlc.arg('operator_grn'),
        sqlc.narg('audience_grn'), sqlc.arg('issued_at'), sqlc.arg('expires_at'));

-- The read every request presenting a token takes (ADR-0100,
-- ADR-0102): the token's row joined to the registration it rests on
-- and, for one exchanged under a key, to that key while it is live, so
-- a deleted operator, a re-issued credential, a withdrawn key and a
-- planned rotation's passed deadline each refuse it from the next
-- request.
-- name: ResolveOperatorToken :one
SELECT t.operator_grn, t.audience_grn, t.expires_at
FROM operator_tokens t
JOIN operators o ON o.grn = t.operator_grn
LEFT JOIN operator_keys k ON k.id = t.key_id
WHERE t.token_hash = $1 AND t.expires_at > now()
  AND (t.key_id IS NULL
       OR (k.revoked_at IS NULL AND (k.retires_at IS NULL OR k.retires_at > now())));

-- An exchange holds the credential issuance's row while it records and
-- trims, so two exchanges at once cannot leave more live tokens than
-- the bound. A row gone since its holder was read answers no row.
-- name: LockOperatorCredential :one
SELECT id FROM operator_credentials WHERE id = $1 FOR UPDATE;

-- Keeps the issuance's newest live tokens and drops the rest, which
-- bounds what a leaked credential can write (ADR-0100). It takes them
-- in token_hash order, and is the one statement of an exchange that
-- waits on a token (#1424).
-- name: TrimOperatorTokens :exec
WITH doomed AS MATERIALIZED (
    SELECT t.token_hash FROM operator_tokens t
    WHERE t.credential_id = sqlc.arg('credential_id')::bigint
      AND t.token_hash NOT IN (
          SELECT k.token_hash FROM operator_tokens k
          WHERE k.credential_id = sqlc.arg('credential_id')::bigint AND k.expires_at > now()
          ORDER BY k.issued_at DESC, k.token_hash DESC
          LIMIT sqlc.arg('keep'))
    ORDER BY t.token_hash
    FOR UPDATE OF t)
DELETE FROM operator_tokens WHERE token_hash IN (SELECT token_hash FROM doomed);

-- An exchange sweeps the operator's expired tokens last, so the table
-- stays proportional to what is live without a job of its own. It
-- skips a token another transaction holds, which that transaction is
-- deleting or a later sweep takes, so it never waits (#1424).
-- name: SweepOperatorTokens :exec
DELETE FROM operator_tokens WHERE token_hash IN (
    SELECT t.token_hash FROM operator_tokens t
    WHERE t.operator_grn = sqlc.arg('operator_grn') AND t.expires_at <= now()
    FOR UPDATE OF t SKIP LOCKED);

-- Holds the operator's registration while its keys change, so two
-- issuances at once cannot both find room under the bound (ADR-0102).
-- name: LockOperatorRegistration :one
SELECT grn FROM operators WHERE grn = $1 FOR UPDATE;

-- Whether the operator has ever been issued a key, withdrawn ones
-- included: what a first issuance is refused on (ADR-0102).
-- name: OperatorWasKeyed :one
SELECT EXISTS (SELECT 1 FROM operator_keys WHERE operator_grn = $1);

-- The keys an operator holds live at the database's time: neither
-- withdrawn nor past a planned rotation's deadline.
-- name: ListLiveOperatorKeys :many
SELECT id, retires_at FROM operator_keys
WHERE operator_grn = $1 AND revoked_at IS NULL AND (retires_at IS NULL OR retires_at > now())
ORDER BY issued_at, id;

-- A planned rotation starts the predecessor's rollover window, and
-- leaves an earlier deadline where one is already set.
-- name: RetireOperatorKey :exec
UPDATE operator_keys SET retires_at = now() + sqlc.arg('window_seconds')::bigint * interval '1 second'
WHERE id = sqlc.arg('id') AND retires_at IS NULL;

-- Withdraws every live key of the operator and answers which, so the
-- same write deletes their tokens.
-- name: RevokeLiveOperatorKeys :many
UPDATE operator_keys SET revoked_at = now()
WHERE operator_grn = $1 AND revoked_at IS NULL AND (retires_at IS NULL OR retires_at > now())
RETURNING id;

-- Withdraws one key the operator holds, live or past its deadline, and
-- answers its row; a key already withdrawn answers none.
-- name: RevokeOperatorKey :one
UPDATE operator_keys SET revoked_at = now()
WHERE operator_grn = sqlc.arg('operator_grn') AND kid = sqlc.arg('kid') AND revoked_at IS NULL
RETURNING id;

-- name: OperatorKeyExists :one
SELECT EXISTS (SELECT 1 FROM operator_keys WHERE operator_grn = sqlc.arg('operator_grn') AND kid = sqlc.arg('kid'));

-- name: DeleteOperatorKeyTokens :exec
WITH doomed AS MATERIALIZED (
    SELECT t.token_hash FROM operator_tokens t
    WHERE t.key_id = ANY(sqlc.arg('key_ids')::bigint[])
    ORDER BY t.token_hash
    FOR UPDATE OF t)
DELETE FROM operator_tokens WHERE token_hash IN (SELECT token_hash FROM doomed);

-- name: InsertOperatorKey :one
INSERT INTO operator_keys (operator_grn, kid, algorithm, public_jwk, issued_by, issued_at)
VALUES (sqlc.arg('operator_grn'), sqlc.arg('kid'), sqlc.arg('algorithm'), sqlc.arg('public_jwk'),
        sqlc.arg('issued_by'), sqlc.arg('issued_at'))
RETURNING id;

-- Every key the operator was issued, with the state the database's
-- time puts it in.
-- name: ListOperatorKeys :many
SELECT id, operator_grn, kid, algorithm, public_jwk, issued_by, issued_at, retires_at, revoked_at, last_exchanged_at,
       (CASE
            WHEN revoked_at IS NOT NULL OR retires_at <= now() THEN 'revoked'
            WHEN retires_at IS NOT NULL THEN 'retiring'
            ELSE 'active'
        END)::text AS state
FROM operator_keys
WHERE operator_grn = $1
ORDER BY issued_at, id;

-- The live key a client's assertion names, looked up within that
-- client's registration alone (ADR-0102).
-- name: GetLiveOperatorKey :one
SELECT k.id, k.operator_grn, k.kid, k.algorithm, k.public_jwk, k.issued_by, k.issued_at,
       k.retires_at, k.revoked_at, k.last_exchanged_at
FROM operator_keys k
JOIN operators o ON o.grn = k.operator_grn
WHERE k.operator_grn = sqlc.arg('operator_grn') AND k.kid = sqlc.arg('kid')
  AND k.revoked_at IS NULL AND (k.retires_at IS NULL OR k.retires_at > now());

-- An exchange holds its registration, and its audience's, before the
-- key or credential and the tokens, the order an issuance or a deletion
-- takes them in, so neither waits on the other while holding what the
-- other needs (#1399). It holds the two in GRN byte order, as an
-- organization's deletion holds all of them (#1424). KEY SHARE is the lock the token's foreign keys
-- take anyway: it waits for an issuance or a deletion of the operator,
-- and for nothing else.
-- name: HoldOperatorRegistration :one
SELECT grn FROM operators WHERE grn = $1 FOR KEY SHARE;

-- An exchange holds the verifying key's row, rereading that it is
-- still live, under the lock a withdrawal's update takes too: a
-- withdrawal either commits first and this answers no row, or waits
-- and deletes the token this exchange records. It answers the
-- database's time, which the assertion's dates are measured against.
-- name: HoldLiveOperatorKey :one
SELECT now()::timestamptz AS decided_at
FROM operator_keys
WHERE id = sqlc.arg('id') AND operator_grn = sqlc.arg('operator_grn')
  AND revoked_at IS NULL AND (retires_at IS NULL OR retires_at > now())
FOR NO KEY UPDATE;

-- Drops the client's spent identifiers past their keeping. It is a
-- statement of its own rather than the spend's CTE, which would share
-- the spend's snapshot and still conflict with a row it deletes.
-- name: SweepOperatorAssertions :exec
DELETE FROM operator_assertions WHERE operator_grn = $1 AND expires_at <= now();

-- Spends a (client, jti). A conflict answers no row: the assertion was
-- spent already, by this replica or another (ADR-0102).
-- name: SpendOperatorAssertion :one
INSERT INTO operator_assertions (operator_grn, jti, expires_at)
VALUES (sqlc.arg('operator_grn'), sqlc.arg('jti'), sqlc.arg('expires_at'))
ON CONFLICT (operator_grn, jti) DO NOTHING
RETURNING jti;

-- name: RecordOperatorKeyToken :exec
INSERT INTO operator_tokens (token_hash, key_id, operator_grn, audience_grn, issued_at, expires_at)
VALUES (sqlc.arg('token_hash'), sqlc.arg('key_id')::bigint, sqlc.arg('operator_grn'),
        sqlc.narg('audience_grn'), sqlc.arg('issued_at'), sqlc.arg('expires_at'));

-- Keeps the key's newest live tokens and drops the rest, the bound and
-- the order a credential's trim has too.
-- name: TrimOperatorKeyTokens :exec
WITH doomed AS MATERIALIZED (
    SELECT t.token_hash FROM operator_tokens t
    WHERE t.key_id = sqlc.arg('key_id')::bigint
      AND t.token_hash NOT IN (
          SELECT k.token_hash FROM operator_tokens k
          WHERE k.key_id = sqlc.arg('key_id')::bigint AND k.expires_at > now()
          ORDER BY k.issued_at DESC, k.token_hash DESC
          LIMIT sqlc.arg('keep'))
    ORDER BY t.token_hash
    FOR UPDATE OF t)
DELETE FROM operator_tokens WHERE token_hash IN (SELECT token_hash FROM doomed);

-- name: MarkOperatorKeyExchanged :exec
UPDATE operator_keys SET last_exchanged_at = sqlc.arg('at') WHERE id = sqlc.arg('id');
