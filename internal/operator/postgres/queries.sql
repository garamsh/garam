-- name: CreateOperator :exec
INSERT INTO operators (grn, org_grn, registered_by, created_at, name, description)
VALUES ($1, $2, $3, $4, $5, $6);

-- An absent argument keeps the column it would have written, so a
-- caller changing one of the two leaves the other as it stands. The
-- statement names no other column: the GRN, and the certificate and
-- key rows keyed on it, are what an operator authenticates by, and
-- this write moves none of them (ADR-0074).
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

-- name: GetOperator :one
SELECT * FROM operators WHERE grn = $1;

-- name: ListOperatorsByOrg :many
SELECT * FROM operators WHERE org_grn = $1 ORDER BY grn;

-- Deleting the record is what stops the operator authenticating
-- (ADR-0070).
-- name: DeleteOperator :execrows
DELETE FROM operators WHERE grn = $1;

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
-- organization removes with it. A certificate already issued is not
-- counted:
-- `operator_certificates` carries no foreign key and keeps its row
-- (ADR-0045 errata).
SELECT count(*) FROM operators WHERE org_grn = $1;

-- The read every request presenting a token takes (ADR-0100,
-- ADR-0102): the token's row joined to the registration it rests on
-- and to the key it was exchanged under while that key is live, so a
-- deleted operator, a withdrawn key and a planned rotation's passed
-- deadline each refuse it from the next request.
-- name: ResolveOperatorToken :one
SELECT t.operator_grn, t.audience_grn, t.expires_at
FROM operator_tokens t
JOIN operators o ON o.grn = t.operator_grn
JOIN operator_keys k ON k.id = t.key_id
WHERE t.token_hash = $1 AND t.expires_at > now()
  AND k.revoked_at IS NULL AND (k.retires_at IS NULL OR k.retires_at > now());

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
-- key and the tokens, the order an issuance or a deletion
-- takes them in, so neither waits on the other while holding what the
-- other needs (#1399). It holds the two in GRN byte order, as an
-- organization's deletion holds all of them (#1424). KEY SHARE is the
-- lock the token's foreign keys take anyway: it waits for an issuance
-- or a deletion of the operator, and for nothing else.
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

-- Keeps the key's newest live tokens and drops the rest, which bounds
-- what a leaked key can write (ADR-0102). It takes them in token_hash
-- order, and is the one statement of an exchange that waits on a token
-- (#1424).
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

-- name: GetDelegationByRequest :one
SELECT * FROM operator_delegations
WHERE org_grn = $1 AND operator_grn = $2 AND request_id = $3;

-- A grant write locks the current grant before replacing it, so a
-- second writer holding the same view waits and then finds it ended.
-- name: LockCurrentDelegation :one
SELECT * FROM operator_delegations
WHERE org_grn = $1 AND operator_grn = $2 AND ended_at IS NULL
FOR UPDATE;

-- name: GetCurrentDelegation :one
SELECT * FROM operator_delegations
WHERE org_grn = $1 AND operator_grn = $2 AND ended_at IS NULL;

-- name: GetDelegation :one
SELECT * FROM operator_delegations WHERE id = $1;

-- name: CreateDelegation :exec
INSERT INTO operator_delegations
    (id, org_grn, operator_grn, request_id, replaces, operations, expires_at, granted_by, granted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: AddDelegationControllers :exec
INSERT INTO operator_delegation_controllers (delegation_id, controller_grn)
SELECT sqlc.arg('delegation_id'), unnest(sqlc.arg('controller_grns')::text[]);

-- name: ListDelegationControllers :many
SELECT controller_grn FROM operator_delegation_controllers
WHERE delegation_id = $1
ORDER BY controller_grn;

-- Ends the named grant only, so a withdrawal retried after a
-- replacement never ends the replacement.
-- name: EndDelegation :execrows
UPDATE operator_delegations SET ended_at = sqlc.arg('ended_at')
WHERE id = sqlc.arg('id') AND org_grn = sqlc.arg('org_grn')
  AND operator_grn = sqlc.arg('operator_grn') AND ended_at IS NULL;

-- A request identifier holds one logical operation. A remint of the
-- same binding replaces its token and answers its reference; any other
-- binding under the identifier updates nothing and returns no row,
-- which the caller reads as a conflict. One statement, so concurrent
-- mints of one identifier leave one row.
-- name: MintOperationAuthority :one
INSERT INTO operation_authorities
    (ref, org_grn, audience_grn, request_id, delegation_id, actor_grn, operation,
     target_grn, assignee_grn, assignment_epoch, body_sha256, request_target,
     destination_grn, token_hash, token_expires_at, created_at)
VALUES (sqlc.arg('ref'), sqlc.arg('org_grn'), sqlc.arg('audience_grn'), sqlc.arg('request_id'),
        sqlc.arg('delegation_id'), sqlc.arg('actor_grn'), sqlc.arg('operation'),
        sqlc.arg('target_grn'), sqlc.narg('assignee_grn'), sqlc.narg('assignment_epoch'),
        sqlc.arg('body_sha256'), sqlc.narg('request_target'), sqlc.narg('destination_grn'),
        sqlc.arg('token_hash'),
        sqlc.arg('token_expires_at'),
        sqlc.arg('created_at'))
ON CONFLICT (org_grn, audience_grn, request_id) DO UPDATE
    SET token_hash = EXCLUDED.token_hash, token_expires_at = EXCLUDED.token_expires_at
    WHERE operation_authorities.delegation_id = EXCLUDED.delegation_id
      AND operation_authorities.actor_grn = EXCLUDED.actor_grn
      AND operation_authorities.operation = EXCLUDED.operation
      AND operation_authorities.target_grn = EXCLUDED.target_grn
      AND operation_authorities.body_sha256 = EXCLUDED.body_sha256
      AND operation_authorities.assignee_grn IS NOT DISTINCT FROM EXCLUDED.assignee_grn
      AND operation_authorities.assignment_epoch IS NOT DISTINCT FROM EXCLUDED.assignment_epoch
      AND operation_authorities.request_target IS NOT DISTINCT FROM EXCLUDED.request_target
      AND operation_authorities.destination_grn IS NOT DISTINCT FROM EXCLUDED.destination_grn
RETURNING ref;

-- name: GetOperationAuthorityByToken :one
SELECT * FROM operation_authorities WHERE token_hash = $1;

-- name: GetOperationAuthority :one
SELECT * FROM operation_authorities WHERE ref = $1;
