-- The state an OAuth login issued, held where the server can decide it.
-- Expiring the state cookie asks the browser to drop it, and a replayed
-- callback carrying a captured cookie never obeys; this row is what
-- makes the state single-use — the callback that completes deletes it,
-- and a second one carrying the same state finds nothing to delete.
--
-- Only the hash is stored, as `sessions` stores only the hash of a
-- bearer token (ADR-0005): a leaked database holds no state a callback
-- could be built around.
CREATE TABLE oauth_states (
    state_hash text PRIMARY KEY,
    expires_at timestamptz NOT NULL
);

-- Issuing a state deletes the states that have expired, so the table
-- holds the logins still in flight rather than every login ever
-- started. This index is what that sweep reads.
CREATE INDEX oauth_states_expires_at_idx ON oauth_states (expires_at);
