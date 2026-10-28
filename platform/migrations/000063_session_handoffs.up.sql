-- A person signs in once, on the site, and each other app garam serves
-- receives a session of its own on its own host by a one-time handoff
-- (ADR-0131): the session cookie stays host-only.
--
-- handed_off_from names the session a handed-off one was made from:
-- the sign-in, which is always a session no handoff made. Deleting the
-- sign-in deletes every session handed off from it, so signing out on
-- any host ends the sign-in on all of them.
ALTER TABLE sessions
    ADD COLUMN handed_off_from uuid REFERENCES sessions (id) ON DELETE CASCADE,
    ADD CONSTRAINT sessions_handed_off_is_browser CHECK (
        handed_off_from IS NULL OR kind = 'browser');

CREATE INDEX sessions_handed_off_from_idx ON sessions (handed_off_from);

-- One handoff in flight: the code the receiving app redeems and the
-- state that app holds in a cookie of its own, both as hashes, and the
-- sign-in it hands off. Redeeming deletes the row, so a code buys one
-- session; it lives a minute, and issuing one sweeps the lapsed.
CREATE TABLE session_handoffs (
    code_hash  text PRIMARY KEY,
    state_hash text NOT NULL,
    session_id uuid NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

CREATE INDEX session_handoffs_expires_at_idx ON session_handoffs (expires_at);
CREATE INDEX session_handoffs_session_id_idx ON session_handoffs (session_id);
