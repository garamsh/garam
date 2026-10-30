ALTER TABLE policies DROP CONSTRAINT policies_default_version_fkey;
DROP TABLE policy_versions;
DROP FUNCTION policy_versions_append_only();
DROP TABLE policies;
