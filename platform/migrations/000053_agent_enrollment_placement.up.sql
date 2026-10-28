-- A standalone approval names where the host runs its agent's runtime
-- (ADR-0111): 'standalone', the three containers the supervisor
-- starts (ADR-0085), or 'direct', a runtime already running on the
-- host that the supervisor attaches to and starts nothing of. Every
-- approval written before this migration was a standalone one.
--
-- A direct host enrolls under standalone-enrollment.v2 alone: the
-- supervisor hands the adapter tokens, and the attach mode copies no
-- certificate anywhere, so a direct approval binds no key.
--
-- A replacement's spending records the agent's most recent activation
-- as it stood under the execution lock, which the replacement ended or
-- found ended. The new supervisor's first activation must name it, and
-- nothing else tells a host with a fresh record what it is; a replay
-- answers the recorded one, never one activated since. It is NULL for
-- a new agent, and for a replacement of an agent never activated.
ALTER TABLE agent_enrollments
    ADD COLUMN placement text NOT NULL DEFAULT 'standalone'
        CHECK (placement IN ('standalone', 'direct')),
    ADD COLUMN replaced_activation_id uuid,
    ADD CONSTRAINT agent_enrollments_direct_is_keyless
        CHECK (placement = 'standalone' OR supervisor_key_sha256 IS NULL),
    ADD CONSTRAINT agent_enrollments_replaced_activation_is_spent
        CHECK (replaced_activation_id IS NULL OR (consumed_at IS NOT NULL AND expected_assignee_grn IS NOT NULL));
