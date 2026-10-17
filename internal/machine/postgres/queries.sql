-- RecordSighting upserts one caller's row for one path (ADR-0109). The
-- organization is read rather than trusted, so a sighting for one that
-- is gone writes nothing. An issuance that arrives while the token the
-- row last recorded is still accepted is a refresh. The database's
-- clock stamps it.
-- name: RecordSighting :exec
INSERT INTO machine_authentication_evidence (caller_grn, path, org_grn, first_at, last_at, issued_until)
SELECT sqlc.arg('caller_grn')::text, sqlc.arg('path')::text, grn,
       clock_timestamp(), clock_timestamp(), sqlc.narg('issued_until')::timestamptz
FROM orgs
WHERE grn = sqlc.arg('org_grn')::text
ON CONFLICT (caller_grn, path) DO UPDATE SET
    last_at         = EXCLUDED.last_at,
    issued_until    = GREATEST(machine_authentication_evidence.issued_until, EXCLUDED.issued_until),
    last_refresh_at = CASE
        WHEN machine_authentication_evidence.issued_until > EXCLUDED.last_at THEN EXCLUDED.last_at
        ELSE machine_authentication_evidence.last_refresh_at
    END;
