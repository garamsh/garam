-- The two tables 000001 created with an expiry never removed a row that
-- reached it: `GetSession` filters expired sessions out and an expired
-- invitation is refused on read, so both went on holding every row ever
-- written. Creating one of each now deletes the rows of its own table
-- that have expired, as issuing an OAuth state does (000012) — a row is
-- written in one place per table, so sweeping there keeps each table
-- proportional to what is live rather than to every sign-in and every
-- invitation ever made. These indexes are what those sweeps read.
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- An accepted invitation is not swept. It records which invitation a
-- membership came from and who issued it, which outlives the token it
-- was accepted with; a pending one that expired is a spent capability
-- nobody decided to keep. The sweep therefore reads pending rows alone,
-- and so does this index.
CREATE INDEX invitations_pending_expires_at_idx ON invitations (expires_at)
    WHERE accepted_at IS NULL;
