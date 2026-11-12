-- Drops the principals 000073 added, which are the whole table: no
-- earlier migration reads it, so nothing else is restored. An
-- assertion a principal spent stays in operator_assertions, which
-- references no record and clears by its own expiry.
DROP TABLE service_principals;
