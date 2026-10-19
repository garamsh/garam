DROP TABLE device_authorizations;

DROP INDEX sessions_user_grn_idx;

-- A CLI session cannot be read without its columns, so it goes.
DELETE FROM sessions WHERE kind = 'cli';

ALTER TABLE sessions
    DROP CONSTRAINT sessions_refresh_is_cli,
    DROP CONSTRAINT sessions_name,
    DROP CONSTRAINT sessions_kind,
    DROP CONSTRAINT sessions_refresh_hash_unique,
    DROP CONSTRAINT sessions_id_unique,
    DROP COLUMN refresh_expires_at,
    DROP COLUMN refresh_hash,
    DROP COLUMN name,
    DROP COLUMN kind,
    DROP COLUMN id;
