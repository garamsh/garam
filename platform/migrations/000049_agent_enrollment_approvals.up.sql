-- An approval agent-enrollment.v1 admits one agent under (ADR-0102):
-- the binding the hosted control service prepared, and, once
-- admission spent it, the agent it admitted.
--
-- The binding is the member's agent:create reference, the request
-- identifier it was minted under, the placement and the assignee. One
-- reference backs one approval, and one request identifier names one
-- approval in its organization. Admission writes the agent through
-- the same transaction as its managed creation row, so a reference a
-- managed-enrollment creation already spent admits nothing here.
-- Direct placement is refused before anything is written, until #1355
-- decides writer proof, so 'managed' is the only placement stored.
CREATE TABLE agent_enrollment_approvals (
    id            uuid PRIMARY KEY,
    org_grn       text NOT NULL,
    request_id    text NOT NULL CHECK (request_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
    placement     text NOT NULL CHECK (placement = 'managed'),
    audience_grn  text NOT NULL,
    assignee_grn  text NOT NULL,
    operation_ref text NOT NULL,
    created_at    timestamptz NOT NULL,
    -- What admission wrote; NULL until it spends the approval.
    agent_grn     text REFERENCES agent_managed_creations (agent_grn) ON DELETE CASCADE,
    admitted_at   timestamptz,

    CONSTRAINT agent_enrollment_approvals_request UNIQUE (org_grn, request_id),
    CONSTRAINT agent_enrollment_approvals_reference UNIQUE (operation_ref),
    CONSTRAINT agent_enrollment_approvals_agent UNIQUE (agent_grn),
    CONSTRAINT agent_enrollment_approvals_admission_is_whole
        CHECK ((agent_grn IS NULL) = (admitted_at IS NULL))
);
