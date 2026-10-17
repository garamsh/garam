-- One row per message send garam decided (ADR-0106): who sent, to
-- whom, whether it was authorized, and the message it stored when it
-- was. No column holds a body, and none ever has: the record outlives
-- the queue's thirty-day window and the agents themselves, so it keeps
-- the routing and never the content.
--
-- No sweep reaches it. A tenant's rows go with the tenant, so each
-- party's organization is referenced and an organization that is
-- deleted takes every event naming it, on either side. A party whose
-- organization garam does not hold — the recipient of a refused
-- crossing, which may name any organization — references none, and
-- the event is still written.
CREATE TABLE audit_send_events (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sender_grn         text NOT NULL,
    recipient_grn      text NOT NULL,
    sender_org         text REFERENCES orgs (grn) ON DELETE CASCADE,
    recipient_org      text REFERENCES orgs (grn) ON DELETE CASCADE,
    decision           text NOT NULL,
    message_id         text,
    message_created_at timestamptz,
    recorded_at        timestamptz NOT NULL,
    CONSTRAINT audit_send_events_decision CHECK (decision IN ('authorized', 'refused')),
    CONSTRAINT audit_send_events_message CHECK (
        (decision = 'authorized' AND message_id IS NOT NULL AND message_created_at IS NOT NULL)
        OR (decision = 'refused' AND message_id IS NULL AND message_created_at IS NULL)
    )
);

-- An organization's deletion finds the events naming it on either
-- side through these.
CREATE INDEX audit_send_events_sender_org_idx ON audit_send_events (sender_org);
CREATE INDEX audit_send_events_recipient_org_idx ON audit_send_events (recipient_org);
