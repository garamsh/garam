DROP TABLE session_handoffs;

ALTER TABLE sessions
    DROP CONSTRAINT sessions_handed_off_is_browser,
    DROP COLUMN handed_off_from;
