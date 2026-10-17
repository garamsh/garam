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
