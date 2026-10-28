DROP TRIGGER IF EXISTS policy_attachments_notify_change ON policy_attachments;
DROP FUNCTION IF EXISTS policy_attachments_notify_change();
DROP TRIGGER IF EXISTS policy_attachments_same_org ON policy_attachments;
DROP FUNCTION IF EXISTS policy_attachments_same_org();
DROP TABLE IF EXISTS policy_attachments;
