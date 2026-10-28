-- RecordSend writes one send event. Each party's organization is
-- referenced only where garam holds it, so a send naming an
-- organization nobody created is still recorded (ADR-0106). The
-- database's clock stamps it as the statement runs, after the fence's
-- lock wait the transaction may have spent.
-- name: RecordSend :exec
INSERT INTO audit_send_events (
    sender_grn, recipient_grn, sender_org, recipient_org,
    decision, message_id, message_created_at, recorded_at
)
VALUES (
    sqlc.arg('sender_grn'), sqlc.arg('recipient_grn'),
    (SELECT grn FROM orgs WHERE grn = sqlc.arg('sender_org')::text),
    (SELECT grn FROM orgs WHERE grn = sqlc.arg('recipient_org')::text),
    sqlc.arg('decision'), sqlc.narg('message_id'), sqlc.narg('message_created_at'),
    clock_timestamp()
);

-- RecordCredential writes one machine credential event (ADR-0114),
-- stamped by the database's clock.
-- name: RecordCredential :exec
INSERT INTO audit_credential_events (
    subject_grn, org_grn, credential, action, outcome, replay, reason,
    serial, not_before, not_after, recorded_at
)
VALUES (
    sqlc.arg('subject_grn'), sqlc.arg('org_grn'), sqlc.arg('credential'), sqlc.arg('action'),
    sqlc.arg('outcome'), sqlc.arg('replay'), sqlc.narg('reason'), sqlc.narg('serial'),
    sqlc.narg('not_before'), sqlc.narg('not_after'), clock_timestamp()
);

-- ListCredentials pages one subject's events, newest first.
-- name: ListCredentials :many
SELECT subject_grn, credential, action, outcome, replay, reason, serial, not_before, not_after, recorded_at
FROM audit_credential_events
WHERE org_grn = sqlc.arg('org_grn') AND subject_grn = sqlc.arg('subject_grn')
ORDER BY recorded_at DESC, id DESC
LIMIT sqlc.narg('row_limit') OFFSET sqlc.arg('row_offset');
