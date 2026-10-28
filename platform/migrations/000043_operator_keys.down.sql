-- A token exchanged under a key has no credential to restore, so it is
-- deleted rather than left refusing the restored NOT NULL.
DELETE FROM operator_tokens WHERE key_id IS NOT NULL;

ALTER TABLE operator_tokens
    DROP CONSTRAINT operator_tokens_one_issuance,
    DROP COLUMN key_id,
    ALTER COLUMN credential_id SET NOT NULL;

DROP TABLE operator_assertions;
DROP TABLE operator_keys;
