-- A watch is one person's live view of one agent, open only while that
-- person's stream is (ADR-0022). The row exists so a poll can be told
-- how many are watching: the stream is held by `serve api` and the poll
-- is answered by `serve machine`, which are separate processes
-- (ADR-0017), so neither can read the other's memory.
--
-- Nothing about a turn is stored here or anywhere else. The events pass
-- through a notification and are never written: the live view is best
-- effort and never a record (ADR-0022).
--
-- The lease is here for the reason a claim's is: the process holding a
-- stream can die without deleting anything, and a row nobody expires
-- would have an adapter forwarding to a watcher that is gone — the one
-- cost ADR-0022 says an agent nobody watches must not pay. Postgres
-- measures it, so no instance's clock decides when a watch lapses.
CREATE TABLE agent_watches (
    id         text PRIMARY KEY,
    agent_grn  text NOT NULL,
    expires_at timestamptz NOT NULL,

    -- What is watched is an agent. No foreign key names one: a watch
    -- may never be what keeps an agent's row alive, and `messages`
    -- addresses its recipient the same way (000008).
    CONSTRAINT agent_watches_agent_service_type
        CHECK (split_part(agent_grn, ':', 4) = 'agent')
);

-- The count a poll reads is per agent and live watches only. The sweep
-- that drops lapsed rows reads expiry alone and scans instead: the table
-- holds a row per stream open across the deployment, so there is no
-- second index to keep current for it.
CREATE INDEX agent_watches_live_idx ON agent_watches (agent_grn, expires_at);
