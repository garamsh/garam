-- The earlier policy engine is removed with the tables only it read
-- and wrote (ADR-0177). Nothing stored in them is converted: garam is
-- not operating (ADR-0117), and the policy service holds the
-- organization's policies now.
--
-- Dropping a table drops its triggers. The four functions those
-- triggers ran are dropped by name. The channel they announced on,
-- garam_policy_change, has no writer left.
DROP TABLE policy_attachments;
DROP TABLE policy_versions;
DROP TABLE policies;

DROP FUNCTION policy_attachments_notify_change();
DROP FUNCTION policy_attachments_same_org();
DROP FUNCTION policy_versions_append_only();
DROP FUNCTION policies_notify_change();
