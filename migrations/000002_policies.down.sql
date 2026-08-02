DROP TRIGGER IF EXISTS policies_notify_change ON policies;
DROP FUNCTION IF EXISTS policies_notify_change();
DROP TRIGGER IF EXISTS policy_versions_append_only ON policy_versions;
DROP FUNCTION IF EXISTS policy_versions_append_only();
DROP TABLE IF EXISTS policy_versions;
DROP TABLE IF EXISTS policies;
