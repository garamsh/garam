-- A person signs the CLI in by the device grant (RFC 8628), and the
-- CLI holds a session presented as a bearer (ADR-0120).
--
-- A CLI session is a row of sessions like any other: kind tells it
-- apart, and its refresh token, stored as its hash alone, buys the next
-- access token. id names a session to its user, who lists and revokes
-- them, without the token's hash ever leaving the table.
ALTER TABLE sessions
    ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN kind text NOT NULL DEFAULT 'browser',
    ADD COLUMN name text,
    ADD COLUMN refresh_hash text,
    ADD COLUMN refresh_expires_at timestamptz,
    ADD CONSTRAINT sessions_id_unique UNIQUE (id),
    ADD CONSTRAINT sessions_refresh_hash_unique UNIQUE (refresh_hash),
    ADD CONSTRAINT sessions_kind CHECK (kind IN ('browser', 'cli')),
    ADD CONSTRAINT sessions_name CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 100),
    ADD CONSTRAINT sessions_refresh_is_cli CHECK (
        (refresh_hash IS NULL) = (refresh_expires_at IS NULL)
        AND (refresh_hash IS NULL OR kind = 'cli'));

CREATE INDEX sessions_user_grn_idx ON sessions (user_grn);

-- One device authorization: the codes the CLI was answered, both as
-- hashes, until an approved one is redeemed or it lapses. A row past
-- its expiry is kept an hour, so a late poll is answered expired_token
-- rather than an unknown code.
CREATE TABLE device_authorizations (
    device_code_hash text PRIMARY KEY,
    user_code_hash   text NOT NULL UNIQUE,
    client_name      text CHECK (client_name IS NULL OR char_length(client_name) BETWEEN 1 AND 100),
    interval_seconds integer NOT NULL CHECK (interval_seconds > 0),
    created_at       timestamptz NOT NULL,
    expires_at       timestamptz NOT NULL,
    last_polled_at   timestamptz,
    decision         text CHECK (decision IN ('approved', 'denied')),
    user_grn         text REFERENCES users (grn) ON DELETE CASCADE,
    decided_at       timestamptz,
    CONSTRAINT device_authorizations_decision_is_whole CHECK (
        (decision IS NULL) = (user_grn IS NULL)
        AND (decision IS NULL) = (decided_at IS NULL))
);

CREATE INDEX device_authorizations_expires_at_idx ON device_authorizations (expires_at);
