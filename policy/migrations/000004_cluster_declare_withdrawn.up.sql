-- iam:cluster.declare is withdrawn without a mapping: placing a
-- declaration is iam:bot.place on the instance (ADR-0192 decision 10,
-- ADR-0193, #2082). A stored permit naming it would go on granting
-- nothing, without a word to the organization that wrote it, so every
-- permit any version of which names it goes, as a purge takes a policy
-- naming a removed entity: a version is never rewritten, and one that
-- is not the default would decide again on a rollback. A permit naming
-- it beside other actions goes whole, which only takes grants away.
-- A guardrail stays: a forbid of an action no manifest declares
-- decides nothing, and deleting one naming another action beside it
-- would lift that action's deny. Each removal is recorded as the
-- service's own purge records it, where the organization is held.
WITH withdrawn AS (
    DELETE FROM policies p
    WHERE EXISTS (
        SELECT 1 FROM policy_versions v
        WHERE v.policy_grn = p.grn
          AND v.body->>'kind' = 'permit'
          AND v.body->>'source' ~ 'Iam\s*::\s*Action\s*::\s*"cluster\.declare"'
    )
    RETURNING p.grn, p.org_grn
)
INSERT INTO audit_records (org_grn, kind, service, action, resource, outcome, policy_ids)
SELECT w.org_grn, 'write', 'policy', 'iam:policy.delete', w.grn, 'removed', ARRAY[w.grn]
FROM withdrawn w
WHERE EXISTS (
    SELECT 1 FROM entities e
    WHERE e.entity_type = 'Iam::Org' AND e.entity_id = w.org_grn AND e.document IS NOT NULL
);
