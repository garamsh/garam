-- iam:cluster.declare is withdrawn without a mapping: placing a
-- declaration is iam:bot.place on the instance (ADR-0192 decision 10,
-- ADR-0193, #2082). A stored policy naming it would go on matching
-- nothing, without a word to the organization that wrote it, so every
-- policy any version of which names it goes, as a purge takes a policy
-- naming a removed entity: a version is never rewritten, and one that
-- is not the default would decide again on a rollback. A policy naming
-- it beside other actions goes whole, and its organization writes those
-- again.
DELETE FROM policies p
WHERE EXISTS (
    SELECT 1 FROM policy_versions v
    WHERE v.policy_grn = p.grn
      AND v.body->>'source' ~ 'Iam\s*::\s*Action\s*::\s*"cluster\.declare"'
);
